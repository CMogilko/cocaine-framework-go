package cocaine12

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/net/context"
)

const (
	TraceInfoValue      = "trace.traceinfo"
	TraceStartTimeValue = "trace.starttime"
)

var (
	initTraceLogger sync.Once
	traceLogger     Logger
)

func traceLog() Logger {
	initTraceLogger.Do(func() {
		var err error
		traceLogger, err = NewLogger(context.Background())
		// there must be no error
		if err != nil {
			panic(fmt.Sprintf("unable to create trace logger: %v", err))
		}
	})
	return traceLogger
}

func getTraceInfo(ctx context.Context) *TraceInfo {
	if val, ok := ctx.Value(TraceInfoValue).(TraceInfo); ok {
		return &val
	}
	return nil
}

// CloseSpan closes attached span. It should be call after
// the rpc ends.
type CloseSpan func(format string, args ...interface{})

var (
	closeDummySpan CloseSpan = func(format string, args ...interface{}) {}
)

type TraceInfo struct {
	trace, span, parent uint64
}

type traced struct {
	context.Context
	traceInfo TraceInfo
	startTime time.Time
}

func (t *traced) Value(key interface{}) interface{} {
	switch key {
	case TraceInfoValue:
		return t.traceInfo
	case TraceStartTimeValue:
		return t.startTime
	default:
		return t.Context.Value(key)
	}
}

// It might be used in client applications.
func BeginNewTraceContext(ctx context.Context) context.Context {
	ts := uint64(rand.Int63())
	return AttachTraceInfo(ctx, TraceInfo{
		trace:  ts,
		span:   ts,
		parent: 0,
	})
}

// AttachTraceInfo binds given TraceInfo to the context.
// If ctx is nil, then TraceInfo will be attached to context.Background()
func AttachTraceInfo(ctx context.Context, traceInfo TraceInfo) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return &traced{
		Context:   ctx,
		traceInfo: traceInfo,
		startTime: time.Now(),
	}
}

// CleanTraceInfo might be used to clear context instance from trace info
// to disable tracing in some RPC calls to get rid of overhead
func CleanTraceInfo(ctx context.Context) context.Context {
	return context.WithValue(ctx, TraceInfoValue, nil)
}

// WithTrace starts new span and returns a context with attached TraceInfo and Done.
// If ctx is nil or has no TraceInfo new span won't start to support sampling,
// so it's user responsibility to make sure that the context has TraceInfo.
// Anyway it safe to call CloseSpan function even in this case, it actually does nothing.
func WithTrace(ctx context.Context, rpcName string) (context.Context, func(format string, args ...interface{})) {
	if ctx == nil {
		// I'm not sure it is a valid action.
		// According to the rule "no trace info, no new span"
		// to support sampling, nil Context has no TraceInfo, so
		// it cannot start new Span.
		return context.Background(), closeDummySpan
	}

	traceInfo := getTraceInfo(ctx)
	if traceInfo == nil {
		// given context has no TraceInfo
		// so we can't start new trace to support sampling.
		// closeDummySpan does nohing
		return ctx, closeDummySpan
	}

	// startTime is not used only to log the start of an RPC
	// It's stored in Context to calculate the RPC call duration.
	// A user can get it via Context.Value(TraceStartTimeValue)
	startTime := time.Now()

	// Tracing magic:
	// * the previous span becomes our parent
	// * new span is set as random number
	// * trace still stays the same
	traceInfo.parent = traceInfo.span
	traceInfo.span = uint64(rand.Int63())

	traceLog().WithFields(Fields{
		"trace_id":  fmt.Sprintf("%x", traceInfo.trace),
		"span_id":   fmt.Sprintf("%x", traceInfo.span),
		"parent_id": fmt.Sprintf("%x", traceInfo.parent),
		"timestamp": startTime.UnixNano(),
		"RPC":       rpcName,
	}).Infof("start")

	ctx = &traced{
		Context:   ctx,
		traceInfo: *traceInfo,
		startTime: startTime,
	}

	return ctx, func(format string, args ...interface{}) {
		now := time.Now()
		duration := now.Sub(startTime)
		traceLog().WithFields(Fields{
			"trace_id":  fmt.Sprintf("%x", traceInfo.trace),
			"span_id":   fmt.Sprintf("%x", traceInfo.span),
			"parent_id": fmt.Sprintf("%x", traceInfo.parent),
			"timestamp": now.UnixNano(),
			"duration":  duration.Nanoseconds() / 1000,
			"RPC":       rpcName,
		}).Infof(format, args...)
	}
}
