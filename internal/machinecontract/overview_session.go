package machinecontract

// Runtime resource limits, not persisted project policy or cognition identity.
const (
	OverviewDeliverySessionPrefix         = "v2"
	OverviewDeliverySessionMaxCount       = 32
	OverviewDeliverySessionMaxBytes       = 64 << 20
	OverviewDeliverySessionIdleMillis     = 15 * 60 * 1000
	OverviewDeliverySessionLifetimeMillis = 60 * 60 * 1000
	OverviewDeliveryCursorMaxBytes        = 512
)
