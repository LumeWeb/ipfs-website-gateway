package ipfs

import (
	"testing"
)

// TestParseRangeHeader tests parsing of HTTP Range headers.
func TestParseRangeHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		want     *HTTPRange
		wantErr  bool
	}{
		{
			name:    "valid range with start and end",
			header:  "bytes=0-1023",
			want:    &HTTPRange{Start: 0, End: 1023},
			wantErr: false,
		},
		{
			name:    "valid range with start only",
			header:  "bytes=500-",
			want:    &HTTPRange{Start: 500, End: -1},
			wantErr: false,
		},
		{
			name:    "valid range from end",
			header:  "bytes=-500",
			want:    &HTTPRange{Start: -1, End: 500},
			wantErr: false,
		},
		{
			name:    "empty header returns nil",
			header:  "",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "invalid format - missing bytes prefix",
			header:  "0-1023",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid format - no dash",
			header:  "bytes=1023",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid format - multiple ranges not supported",
			header:  "bytes=0-500,1000-1500",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid - start greater than end",
			header:  "bytes=500-100",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRangeHeader(tt.header)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRangeHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want == nil && got != nil {
				t.Errorf("ParseRangeHeader() = %v, want nil", got)
				return
			}
			if got != nil && tt.want != nil {
				if got.Start != tt.want.Start {
					t.Errorf("ParseRangeHeader().Start = %v, want %v", got.Start, tt.want.Start)
				}
				if got.End != tt.want.End {
					t.Errorf("ParseRangeHeader().End = %v, want %v", got.End, tt.want.End)
				}
			}
		})
	}
}

// TestHTTPRangeValidate tests validation of HTTP ranges against file size.
func TestHTTPRangeValidate(t *testing.T) {
	tests := []struct {
		name    string
		r       *HTTPRange
		size    int64
		want    *HTTPRange
		wantErr bool
	}{
		{
			name:    "valid range within file bounds",
			r:       &HTTPRange{Start: 0, End: 1023},
			size:    2048,
			want:    &HTTPRange{Start: 0, End: 1023},
			wantErr: false,
		},
		{
			name:    "valid range from start to end of file",
			r:       &HTTPRange{Start: 500, End: -1},
			size:    2048,
			want:    &HTTPRange{Start: 500, End: 2047},
			wantErr: false,
		},
		{
			name:    "valid suffix range",
			r:       &HTTPRange{Start: -1, End: 500},
			size:    2048,
			want:    &HTTPRange{Start: 1548, End: 2047},
			wantErr: false,
		},
		{
			name:    "valid suffix range equal to file size",
			r:       &HTTPRange{Start: -1, End: 2048},
			size:    2048,
			want:    &HTTPRange{Start: 0, End: 2047},
			wantErr: false,
		},
		{
			name:    "valid single byte range",
			r:       &HTTPRange{Start: 100, End: 100},
			size:    2048,
			want:    &HTTPRange{Start: 100, End: 100},
			wantErr: false,
		},
		{
			name:    "invalid start beyond file size",
			r:       &HTTPRange{Start: 5000, End: 6000},
			size:    2048,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid start at file size",
			r:       &HTTPRange{Start: 2048, End: -1},
			size:    2048,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid suffix range larger than file",
			r:       &HTTPRange{Start: -1, End: 5000},
			size:    2048,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "nil range returns nil",
			r:       nil,
			size:    2048,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "zero size file with valid start-end range",
			r:       &HTTPRange{Start: 0, End: -1},
			size:    0,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.r.Validate(tt.size)
			if (err != nil) != tt.wantErr {
				t.Errorf("HTTPRange.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want == nil && got != nil {
				t.Errorf("HTTPRange.Validate() = %v, want nil", got)
				return
			}
			if got != nil && tt.want != nil {
				if got.Start != tt.want.Start {
					t.Errorf("HTTPRange.Validate().Start = %v, want %v", got.Start, tt.want.Start)
				}
				if got.End != tt.want.End {
					t.Errorf("HTTPRange.Validate().End = %v, want %v", got.End, tt.want.End)
				}
			}
		})
	}
}
