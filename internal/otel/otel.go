package otel

import (
	"context"
	"sync/atomic"

	"go.lumeweb.com/ipfs-website-gateway/internal/config"

	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func InitTracing(ctx context.Context, cfg config.ObservabilityConfig, version string) error {
	if !cfg.Enabled || cfg.DSN == "" {
		return nil
	}

	options := []uptrace.Option{
		uptrace.WithDSN(cfg.DSN),
		uptrace.WithServiceName(cfg.ServiceName),
		uptrace.WithServiceVersion(version),
	}

	if extraAttrs := detectResourceAttributes(ctx); len(extraAttrs) > 0 {
		options = append(options, uptrace.WithResourceAttributes(extraAttrs...))
	}

	if cfg.IsTracingEnabled() {
		options = append(options,
			uptrace.WithTracingEnabled(true),
			uptrace.WithTraceSampler(
				sdktrace.TraceIDRatioBased(cfg.Tracing.SampleRatio),
			),
		)
	} else {
		options = append(options, uptrace.WithTracingEnabled(false))
	}

	uptrace.ConfigureOpentelemetry(options...)
	return nil
}

func detectResourceAttributes(ctx context.Context) []attribute.KeyValue {
	res, err := resource.New(ctx,
		resource.WithContainer(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
	)
	if err != nil {
		return nil
	}
	var attrs []attribute.KeyValue
	for iter := res.Iter(); iter.Next(); {
		attrs = append(attrs, iter.Attribute())
	}
	return attrs
}

func Shutdown(ctx context.Context) error {
	return uptrace.Shutdown(ctx)
}

func InitLogger(cfg config.ObservabilityConfig, baseLogger *zap.Logger) *zap.Logger {
	if !cfg.IsLoggingEnabled() {
		return baseLogger
	}

	provider := global.GetLoggerProvider()
	if provider == nil {
		return baseLogger
	}

	otelCore := otelzap.NewCore(
		"ipfs-website-gateway",
		otelzap.WithLoggerProvider(provider),
	)

	otelLevelFilter := &levelFilterCore{
		Core: otelCore,
	}
	otelLevelFilter.SetMinLevel(mapLogLevel(cfg.Logging.Level))

	teeCore := zapcore.NewTee(otelLevelFilter, baseLogger.Core())

	return zap.New(teeCore, zap.AddCaller())
}

type levelFilterCore struct {
	zapcore.Core
	minLevel atomic.Int32
}

func (c *levelFilterCore) Enabled(lvl zapcore.Level) bool {
	return lvl >= zapcore.Level(c.minLevel.Load()) && c.Core.Enabled(lvl)
}

func (c *levelFilterCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *levelFilterCore) SetMinLevel(lvl zapcore.Level) {
	c.minLevel.Store(int32(lvl))
}

func mapLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}

// SpanConfig holds configuration for span creation.
type SpanConfig struct {
	Name       string
	Attributes []attribute.KeyValue
	Kind       trace.SpanKind
}

// SpanOption is a function that configures a SpanConfig.
type SpanOption func(*SpanConfig)

// WithAttributes adds attributes to the span.
func WithAttributes(attrs ...attribute.KeyValue) SpanOption {
	return func(c *SpanConfig) {
		c.Attributes = append(c.Attributes, attrs...)
	}
}

// WithSpanKind sets the span kind.
func WithSpanKind(kind trace.SpanKind) SpanOption {
	return func(c *SpanConfig) {
		c.Kind = kind
	}
}

// StartSpan creates and starts a new span with the given configuration.
func StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, trace.Span) {
	config := &SpanConfig{
		Name: name,
		Kind: trace.SpanKindInternal,
	}
	for _, opt := range opts {
		opt(config)
	}
	tracer := otel.Tracer("ipfs-website-gateway")
	return tracer.Start(ctx, config.Name,
		trace.WithAttributes(config.Attributes...),
		trace.WithSpanKind(config.Kind),
	)
}

// TraceMethod starts a new trace span for a method.
func TraceMethod(ctx context.Context, name string, opts ...SpanOption) (context.Context, trace.Span) {
	return StartSpan(ctx, name, opts...)
}

// EndSpanWithErr finishes the span and records the error if non-nil.
func EndSpanWithErr(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, "error")
		span.RecordError(err)
	}
	span.End()
}
