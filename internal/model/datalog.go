package model

// DBHistory markers: models listed here opt into sys_datalog journaling
// (datalog.Tracked), mirroring the tform per-form db_history flag of the
// forms ISPConfig3 datalogs. sys_* bookkeeping tables and the server table
// itself are deliberately absent.

// DBHistory reports that web_domain mutations are datalogged.
func (WebDomain) DBHistory() bool { return true }

// DBHistory reports that web_folder mutations are datalogged.
func (WebFolder) DBHistory() bool { return true }

// DBHistory reports that web_folder_user mutations are datalogged.
func (WebFolderUser) DBHistory() bool { return true }

// DBHistory reports that dns_soa mutations are datalogged.
func (DNSSoa) DBHistory() bool { return true }

// DBHistory reports that dns_rr mutations are datalogged.
func (DNSRr) DBHistory() bool { return true }

// DBHistory reports that dns_slave mutations are datalogged.
func (DNSSlave) DBHistory() bool { return true }

// DBHistory reports that server_ip mutations are datalogged.
func (ServerIP) DBHistory() bool { return true }

// DBHistory reports that server_php mutations are datalogged.
func (ServerPHP) DBHistory() bool { return true }

// DBHistory reports that client mutations are datalogged.
func (Client) DBHistory() bool { return true }
