package otel

import (
	"context"
	"errors"
	"sync/atomic"

	"go.lumeweb.com/ipfs-website-gateway/internal/config"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var (
	tracerProvider *sdktrace.TracerProvider
	loggerProvider *sdklog.LoggerProvider
)

func buildResource(ctx context.Context, cfg config.ObservabilityConfig) (*resource.Resource, error) {
	opts := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
		resource.WithContainer(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
	}

	res, err := resource.New(ctx, opts...)
	if err != nil && res != nil {
		return res, nil
	}
	return res, err
}

func buildTraceExporterOptions(cfg config.OTLPConfig) []otlptracegrpc.Option {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.AuthToken != "" {
		opts = append(opts, otlptracegrpc.WithHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.AuthToken,
		}))
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	return opts
}

func buildLogExporterOptions(cfg config.OTLPConfig) []otlploggrpc.Option {
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.AuthToken != "" {
		opts = append(opts, otlploggrpc.WithHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.AuthToken,
		}))
	}

	if cfg.Insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	return opts
}

func buildSampler(cfg config.TracingConfig) sdktrace.Sampler {
	if cfg.SampleRatio >= 1.0 {
		return sdktrace.AlwaysSample()
	}
	if cfg.SampleRatio <= 0.0 {
		return sdktrace.NeverSample()
	}
	return sdktrace.TraceIDRatioBased(cfg.SampleRatio)
}

func InitTracing(ctx context.Context, cfg config.ObservabilityConfig, version string) error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.OTLP.Endpoint == "" {
		return nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return err
	}

	if cfg.IsTracingEnabled() {
		traceExporter, err := otlptracegrpc.New(ctx, buildTraceExporterOptions(cfg.OTLP)...)
		if err != nil {
			return err
		}

		tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithSampler(buildSampler(cfg.Tracing)),
		)
		otel.SetTracerProvider(tracerProvider)
	} else {
		tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		otel.SetTracerProvider(tracerProvider)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.IsLoggingEnabled() {
		logExporter, err := otlploggrpc.New(ctx, buildLogExporterOptions(cfg.OTLP)...)
		if err != nil {
			return errors.Join(err, tracerProvider.Shutdown(context.Background()))
		}

		loggerProvider = sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		)
		global.SetLoggerProvider(loggerProvider)
	}

	return nil
}

func Shutdown(ctx context.Context) error {
	var errs []error
	if tracerProvider != nil {
		errs = append(errs, tracerProvider.Shutdown(ctx))
	}
	if loggerProvider != nil {
		errs = append(errs, loggerProvider.Shutdown(ctx))
	}
	return errors.Join(errs...)
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
