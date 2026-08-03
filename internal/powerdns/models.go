package powerdns

// Domain is a PowerDNS zone row (table powerdns.domains). ISPConfigID bridges
// to dns_soa.id (MASTER) or dns_slave.id (SLAVE) as written by powerdns_plugin.
type Domain struct {
	ID             int     `gorm:"column:id;primaryKey;autoIncrement"`
	Name           string  `gorm:"column:name"`
	Master         *string `gorm:"column:master"`
	LastCheck      *int    `gorm:"column:last_check"`
	Type           string  `gorm:"column:type"` // MASTER | SLAVE
	NotifiedSerial *int    `gorm:"column:notified_serial"`
	Account        *string `gorm:"column:account"`
	ISPConfigID    int     `gorm:"column:ispconfig_id"`
}

// TableName maps Domain to the PowerDNS domains table.
func (Domain) TableName() string { return "domains" }

// Record is a PowerDNS resource record (table powerdns.records). ISPConfigID
// bridges to dns_rr.id for RRs; the SOA row reuses the zone's dns_soa.id
// (PHP parity).
type Record struct {
	ID          int     `gorm:"column:id;primaryKey;autoIncrement"`
	DomainID    *int    `gorm:"column:domain_id"`
	Name        *string `gorm:"column:name"`
	Type        *string `gorm:"column:type"`
	Content     *string `gorm:"column:content"`
	TTL         *int    `gorm:"column:ttl"`
	Prio        *int    `gorm:"column:prio"`
	ChangeDate  *int    `gorm:"column:change_date"`
	Disabled    *int8   `gorm:"column:disabled;default:0"`
	Auth        *int8   `gorm:"column:auth;default:1"`
	ISPConfigID int     `gorm:"column:ispconfig_id"`
}

// TableName maps Record to the PowerDNS records table.
func (Record) TableName() string { return "records" }

// DomainMetadata is a stub for PowerDNS domainmetadata (DNSSEC etc. managed
// by pdnsutil; the plugin does not write rows here).
type DomainMetadata struct {
	ID       int     `gorm:"column:id;primaryKey;autoIncrement"`
	DomainID int     `gorm:"column:domain_id"`
	Kind     *string `gorm:"column:kind"`
	Content  *string `gorm:"column:content"`
}

// TableName maps DomainMetadata to domainmetadata.
func (DomainMetadata) TableName() string { return "domainmetadata" }

// SuperMaster is a stub for PowerDNS supermasters (not managed by the plugin).
type SuperMaster struct {
	IP         string  `gorm:"column:ip"`
	Nameserver string  `gorm:"column:nameserver"`
	Account    *string `gorm:"column:account"`
}

// TableName maps SuperMaster to supermasters.
func (SuperMaster) TableName() string { return "supermasters" }
