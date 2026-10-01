package rtapi

import (
	"context"
	"fmt"
	"math"
)

// GlobalLimits returns rTorrent's global download and upload rate limits, in
// bytes per second. Zero means unlimited.
func (r *Rtorrent) GlobalLimits() (down, up uint64, err error) {
	return r.GlobalLimitsContext(context.Background())
}

// GlobalLimitsContext is GlobalLimits with a context.
func (r *Rtorrent) GlobalLimitsContext(ctx context.Context) (down, up uint64, err error) {
	req, err := marshalMethodCall(xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{{Value: newArrayValue(
			newMethodCall("throttle.global_down.max_rate", ""),
			newMethodCall("throttle.global_up.max_rate", ""),
		)}},
	})
	if err != nil {
		return 0, 0, err
	}
	resp, err := r.executeMulticall(ctx, req, 2)
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: get global limits: %w", err)
	}
	values, err := resp.arrayParam()
	if err != nil {
		return 0, 0, err
	}
	limits := make([]uint64, 2)
	for i, value := range values[:2] {
		result, err := value.firstArrayValue()
		if err == nil {
			limits[i], err = result.uint64Value()
		}
		if err != nil {
			return 0, 0, fmt.Errorf("rtapi: parse global limit: %w", err)
		}
	}
	return limits[0], limits[1], nil
}

// SetGlobalLimits sets rTorrent's global download and upload rate limits, in
// bytes per second. Zero removes a limit.
func (r *Rtorrent) SetGlobalLimits(down, up uint64) error {
	return r.SetGlobalLimitsContext(context.Background(), down, up)
}

// SetGlobalLimitsContext is SetGlobalLimits with a context.
func (r *Rtorrent) SetGlobalLimitsContext(ctx context.Context, down, up uint64) error {
	if down > math.MaxInt64 || up > math.MaxInt64 {
		return fmt.Errorf("rtapi: rate limit is too large")
	}
	req, err := marshalMethodCall(xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{{Value: newArrayValue(
			newMethodCallValues("throttle.global_down.max_rate.set", newStringValue(""), newIntValue(int64(down))),
			newMethodCallValues("throttle.global_up.max_rate.set", newStringValue(""), newIntValue(int64(up))),
		)}},
	})
	if err != nil {
		return err
	}
	if _, err := r.executeMulticall(ctx, req, 2); err != nil {
		return fmt.Errorf("rtapi: set global limits: %w", err)
	}
	return nil
}
