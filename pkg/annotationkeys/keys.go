package annotationkeys

// 智能限速注解key常量（无-limit后缀）
const (
	RemovedAnnotationKey   = "removed"
	IopsAnnotationKey      = "iops"
	ReadIopsAnnotationKey  = "read-iops"
	WriteIopsAnnotationKey = "write-iops"
	BpsAnnotationKey       = "bps"
	ReadBpsAnnotationKey   = "read-bps"
	WriteBpsAnnotationKey  = "write-bps"
	// Legacy nvme annotation keys
	LegacyIopsAnnotationKey      = "nvme-iops"
	LegacyReadIopsAnnotationKey  = "nvme-iops-read"
	LegacyWriteIopsAnnotationKey = "nvme-iops-write"
	LegacyBpsAnnotationKey       = "nvme-bps"
	LegacyReadBpsAnnotationKey   = "nvme-bps-read"
	LegacyWriteBpsAnnotationKey  = "nvme-bps-write"
)
