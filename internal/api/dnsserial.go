package api

import "time"

// NextSerial ports remote.d/dns.inc.php::increase_serial (design D7): SOA
// serials are YYYYMMDDnn. When the serial's date part is today or newer the
// counter is incremented (nn > 99 rolls into date+1 with nn = 00, purely
// numerically, exactly like the PHP original); an older date resets to
// <today>01. A zero/garbage serial therefore also yields <today>01.
func NextSerial(current uint32, today time.Time) uint32 {
	date := current / 100
	count := current % 100
	todayNum := uint32(today.Year()*10000 + int(today.Month())*100 + today.Day())
	if date >= todayNum {
		count++
		if count > 99 {
			date++
			count = 0
		}
		return date*100 + count
	}
	return todayNum*100 + 1
}
