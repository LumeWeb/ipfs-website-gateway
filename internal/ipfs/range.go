package ipfs

import (
	"fmt"
	"io"
)

// HTTPRange represents an HTTP Range request for byte ranges.
// Start and End are inclusive byte positions.
// If Start is -1, it represents a suffix range (last N bytes).
// If End is -1, it represents a range from Start to end of file.
type HTTPRange struct {
	Start int64
	End   int64
}

// ParseRangeHeader parses an HTTP Range header string.
// Supported formats:
//   - "bytes=start-end" - inclusive range from start to end
//   - "bytes=start-" - range from start to end of file
//   - "bytes=-suffix" - last N bytes of the file
//
// Returns nil if the header is empty.
// Returns error if the format is invalid or multiple ranges are specified.
func ParseRangeHeader(header string) (*HTTPRange, error) {
	if header == "" {
		return nil, nil
	}

	// Check for "bytes=" prefix
	const prefix = "bytes="
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return nil, &RangeError{Message: "invalid range format: missing bytes prefix"}
	}

	rangeSpec := header[len(prefix):]

	// Check for multiple ranges (not supported)
	for _, c := range rangeSpec {
		if c == ',' {
			return nil, &RangeError{Message: "multiple ranges not supported"}
		}
	}

	// Parse the range specification
	var start, end int64
	var err error

	if rangeSpec[0] == '-' {
		// Suffix range: "-suffix" (last N bytes)
		end, err = parseNumber(rangeSpec[1:])
		if err != nil {
			return nil, &RangeError{Message: "invalid suffix range: " + err.Error()}
		}
		if end < 0 {
			return nil, &RangeError{Message: "suffix range must be positive"}
		}
		return &HTTPRange{Start: -1, End: end}, nil
	}

	// Find the dash separator
	dashIndex := -1
	for i, c := range rangeSpec {
		if c == '-' {
			dashIndex = i
			break
		}
	}

	if dashIndex == -1 {
		return nil, &RangeError{Message: "invalid range format: missing dash separator"}
	}

	// Parse start
	start, err = parseNumber(rangeSpec[:dashIndex])
	if err != nil {
		return nil, &RangeError{Message: "invalid start position: " + err.Error()}
	}

	// Parse end (may be empty for "start-" format)
	if dashIndex+1 < len(rangeSpec) {
		end, err = parseNumber(rangeSpec[dashIndex+1:])
		if err != nil {
			return nil, &RangeError{Message: "invalid end position: " + err.Error()}
		}
		if start > end {
			return nil, &RangeError{Message: "start position cannot be greater than end position"}
		}
	} else {
		end = -1
	}

	if start < 0 {
		return nil, &RangeError{Message: "start position cannot be negative"}
	}

	return &HTTPRange{Start: start, End: end}, nil
}

// parseNumber parses a non-negative integer from a string.
func parseNumber(s string) (int64, error) {
	if s == "" {
		return 0, &RangeError{Message: "empty number"}
	}

	var result int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &RangeError{Message: "invalid character in number"}
		}
		result = result*10 + int64(c-'0')
	}

	return result, nil
}

// RangeError represents an error in parsing or validating an HTTP Range request.
type RangeError struct {
	Message string
}

func (e *RangeError) Error() string {
	return "range error: " + e.Message
}

// Validate validates the range against the file size and returns a normalized range.
// For suffix ranges (Start=-1), it calculates the actual start position.
// For open-ended ranges (End=-1), it sets end to size-1.
// Returns an error if the range is invalid or extends beyond the file.
func (r *HTTPRange) Validate(size int64) (*HTTPRange, error) {
	if r == nil {
		return nil, nil
	}

	if size <= 0 {
		return nil, &RangeError{Message: "file size must be positive"}
	}

	normalized := &HTTPRange{}

	if r.Start == -1 {
		// Suffix range: "-suffix" means last N bytes
		if r.End > size {
			return nil, &RangeError{Message: "suffix range exceeds file size"}
		}
		normalized.Start = size - r.End
		normalized.End = size - 1
	} else {
		// Normal range or open-ended range
		if r.Start >= size {
			return nil, &RangeError{Message: "start position exceeds file size"}
		}
		normalized.Start = r.Start

		if r.End == -1 {
			// Open-ended range: "start-" means from start to end of file
			normalized.End = size - 1
		} else {
			// Closed range: "start-end"
			if r.End >= size {
				return nil, &RangeError{Message: "end position exceeds file size"}
			}
			normalized.End = r.End
		}
	}

	return normalized, nil
}

// ContentRange returns the Content-Range header value for this range.
// Format: "bytes start-end/total"
func (r *HTTPRange) ContentRange(total int64) string {
	return "bytes " + formatRange(r.Start, r.End) + "/" + formatNumber(total)
}

// Length returns the length of the range in bytes.
func (r *HTTPRange) Length() int64 {
	return r.End - r.Start + 1
}

func formatRange(start, end int64) string {
	return formatNumber(start) + "-" + formatNumber(end)
}

func formatNumber(n int64) string {
	if n < 0 {
		return "*"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if i == len(buf) {
		return "0"
	}
	return string(buf[i:])
}

// rangeReader wraps an io.ReadSeekCloser to limit reads to a specific range.
type rangeReader struct {
	reader  io.ReadSeekCloser
	remain  int64
	seekPos int64
}

// newRangeReader creates a new range reader that limits reads to the specified length.
func newRangeReader(r io.ReadSeekCloser, length int64) io.ReadSeekCloser {
	return &rangeReader{
		reader: r,
		remain: length,
	}
}

func (rr *rangeReader) Read(p []byte) (int, error) {
	if rr.remain <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > rr.remain {
		p = p[:rr.remain]
	}
	n, err := rr.reader.Read(p)
	rr.remain -= int64(n)
	rr.seekPos += int64(n)
	return n, err
}

func (rr *rangeReader) Seek(offset int64, whence int) (int64, error) {
	// We can only support SeekStart and SeekCurrent with limited functionality
	switch whence {
	case io.SeekStart:
		// Seek to absolute position within the range
		if offset < 0 || int64(offset) > rr.readerSize() {
			return 0, io.ErrUnexpectedEOF
		}
		newPos, err := rr.reader.Seek(offset, io.SeekStart)
		if err != nil {
			return 0, err
		}
		rr.seekPos = newPos
		rr.remain = rr.readerSize() - rr.seekPos
		return rr.seekPos, nil
	case io.SeekCurrent:
		// Seek relative to current position
		newPos := rr.seekPos + offset
		if newPos < 0 || newPos > rr.readerSize() {
			return 0, io.ErrUnexpectedEOF
		}
		actualPos, err := rr.reader.Seek(newPos, io.SeekStart)
		if err != nil {
			return 0, err
		}
		rr.seekPos = actualPos
		rr.remain = rr.readerSize() - rr.seekPos
		return rr.seekPos, nil
	case io.SeekEnd:
		// Seek from end - not supported for range reader
		return 0, io.ErrUnexpectedEOF
	default:
		return 0, fmt.Errorf("invalid whence value")
	}
}

func (rr *rangeReader) Close() error {
	return rr.reader.Close()
}

func (rr *rangeReader) readerSize() int64 {
	// Get the original reader size - this is a bit of a hack
	// but works for our use case where we know the range length
	return rr.seekPos + rr.remain
}
