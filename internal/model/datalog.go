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

// DBHistory reports that mail_domain mutations are datalogged.
func (MailDomain) DBHistory() bool { return true }

// DBHistory reports that mail_user mutations are datalogged.
func (MailUser) DBHistory() bool { return true }

// DBHistory reports that mail_forwarding mutations are datalogged.
func (MailForwarding) DBHistory() bool { return true }

// DBHistory reports that mail_transport mutations are datalogged.
func (MailTransport) DBHistory() bool { return true }

// DBHistory reports that mail_access mutations are datalogged.
func (MailAccess) DBHistory() bool { return true }

// DBHistory reports that spamfilter_users mutations are datalogged.
func (SpamfilterUser) DBHistory() bool { return true }

// DBHistory reports that spamfilter_wblist mutations are datalogged.
func (SpamfilterWblist) DBHistory() bool { return true }

// DBHistory reports that spamfilter_policy mutations are datalogged.
func (SpamfilterPolicy) DBHistory() bool { return true }
