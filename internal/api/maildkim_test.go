package api

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// fakePublisher records calls; zone controls the published result.
type fakePublisher struct {
	zone    bool
	fail    bool
	upserts []string
	deletes []string
}

func (f *fakePublisher) UpsertTXT(_ context.Context, _ *gorm.DB, name, data string, _ uint32) (bool, error) {
	if f.fail {
		return false, errors.New("boom")
	}
	f.upserts = append(f.upserts, name+"|"+data)
	return f.zone, nil
}

func (f *fakePublisher) DeleteTXT(_ context.Context, _ *gorm.DB, name, prefix string) error {
	f.deletes = append(f.deletes, name+"|"+prefix)
	return nil
}

func dkimDomain(domain, selector string, active, dkim bool) *DKIMDomain {
	return &DKIMDomain{
		Domain: domain, Selector: selector, Active: active, DKIM: dkim,
		Public: "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----",
	}
}

func TestSyncDKIMDNS(t *testing.T) {
	t.Run("publish into managed zone", func(t *testing.T) {
		pub := &fakePublisher{zone: true}
		st := SyncDKIMDNS(context.Background(), nil, pub, nil, dkimDomain("example.com", "default", true, true))
		assert.True(t, st.DNSPublished)
		assert.Equal(t, "default._domainkey.example.com. 3600 IN TXT v=DKIM1; t=s; p=AAAA", st.SuggestedRecord)
		assert.Len(t, pub.upserts, 1)
		assert.Empty(t, pub.deletes)
	})

	t.Run("no zone keeps the suggested record only", func(t *testing.T) {
		pub := &fakePublisher{zone: false}
		st := SyncDKIMDNS(context.Background(), nil, pub, nil, dkimDomain("nozone.test", "default", true, true))
		assert.False(t, st.DNSPublished)
		assert.NotEmpty(t, st.SuggestedRecord)
	})

	t.Run("selector change withdraws the old name first", func(t *testing.T) {
		pub := &fakePublisher{zone: true}
		st := SyncDKIMDNS(context.Background(), nil, pub,
			dkimDomain("example.com", "old", true, true),
			dkimDomain("example.com", "new", true, true))
		assert.True(t, st.DNSPublished)
		assert.Equal(t, []string{"old._domainkey.example.com.|v=DKIM1"}, pub.deletes)
	})

	t.Run("dkim disable withdraws and suggests nothing", func(t *testing.T) {
		pub := &fakePublisher{zone: true}
		st := SyncDKIMDNS(context.Background(), nil, pub,
			dkimDomain("example.com", "default", true, true),
			dkimDomain("example.com", "default", true, false))
		assert.False(t, st.DNSPublished)
		assert.Empty(t, st.SuggestedRecord)
		assert.Len(t, pub.deletes, 1)
		assert.Empty(t, pub.upserts)
	})

	t.Run("delete withdraws", func(t *testing.T) {
		pub := &fakePublisher{zone: true}
		SyncDKIMDNS(context.Background(), nil, pub, dkimDomain("example.com", "default", true, true), nil)
		assert.Len(t, pub.deletes, 1)
	})

	t.Run("inactive domain keeps suggestion, publishes nothing", func(t *testing.T) {
		pub := &fakePublisher{zone: true}
		st := SyncDKIMDNS(context.Background(), nil, pub, nil, dkimDomain("example.com", "default", false, true))
		assert.False(t, st.DNSPublished)
		assert.NotEmpty(t, st.SuggestedRecord)
		assert.Empty(t, pub.upserts)
	})

	t.Run("publisher failure degrades to unpublished", func(t *testing.T) {
		pub := &fakePublisher{fail: true}
		st := SyncDKIMDNS(context.Background(), nil, pub, nil, dkimDomain("example.com", "default", true, true))
		assert.False(t, st.DNSPublished)
		assert.NotEmpty(t, st.SuggestedRecord, "save still succeeds with the manual TXT")
	})

	t.Run("nil publisher behaves as noop", func(t *testing.T) {
		st := SyncDKIMDNS(context.Background(), nil, nil, nil, dkimDomain("example.com", "default", true, true))
		assert.False(t, st.DNSPublished)
		assert.NotEmpty(t, st.SuggestedRecord)
	})
}
