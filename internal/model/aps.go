package model

// APSInstance is an installed APS package instance (table aps_instances).
type APSInstance struct {
	ID             int32  `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       int32  `gorm:"column:server_id"`
	CustomerID     int32  `gorm:"column:customer_id"`
	PackageID      int32  `gorm:"column:package_id"`
	InstanceStatus int32  `gorm:"column:instance_status"`
}

// TableName maps APSInstance to the ISPConfig table aps_instances.
func (APSInstance) TableName() string { return "aps_instances" }

// APSInstanceSetting is one name/value setting of an APS instance
// (table aps_instances_settings).
type APSInstanceSetting struct {
	ID         int32  `gorm:"column:id;primaryKey;autoIncrement"`
	ServerID   int32  `gorm:"column:server_id"`
	InstanceID int32  `gorm:"column:instance_id"`
	Name       string `gorm:"column:name"`
	Value      string `gorm:"column:value"`
}

// TableName maps APSInstanceSetting to the ISPConfig table aps_instances_settings.
func (APSInstanceSetting) TableName() string { return "aps_instances_settings" }

// APSPackage is an APS package available in the catalogue
// (table aps_packages).
type APSPackage struct {
	ID            int32  `gorm:"column:id;primaryKey;autoIncrement"`
	Path          string `gorm:"column:path"`
	Name          string `gorm:"column:name"`
	Category      string `gorm:"column:category"`
	Version       string `gorm:"column:version"`
	Release       int32  `gorm:"column:release"`
	PackageURL    string `gorm:"column:package_url"`
	PackageStatus int32  `gorm:"column:package_status"`
}

// TableName maps APSPackage to the ISPConfig table aps_packages.
func (APSPackage) TableName() string { return "aps_packages" }

// APSSetting is a global APS installer setting (table aps_settings).
type APSSetting struct {
	ID    int32  `gorm:"column:id;primaryKey;autoIncrement"`
	Name  string `gorm:"column:name"`
	Value string `gorm:"column:value"`
}

// TableName maps APSSetting to the ISPConfig table aps_settings.
func (APSSetting) TableName() string { return "aps_settings" }
