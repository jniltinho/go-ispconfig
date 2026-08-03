package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rspamdStatSample = `{"version":"3.4","scanned":200,"learned":12,"uptime":3600,
"actions":{"reject":10,"soft reject":2,"rewrite subject":5,"add header":25,
"greylist":8,"no action":150}}`

func TestParseRspamdStat(t *testing.T) {
	data, state, err := ParseRspamdStat([]byte(rspamdStatSample))
	require.NoError(t, err)
	assert.Equal(t, "ok", state)
	assert.Equal(t, uint64(200), data["scanned"])
	assert.Equal(t, uint64(12), data["learned"])
	// reject + add header + rewrite subject
	assert.Equal(t, uint64(40), data["spam"])
	// greylist + soft reject stay out of the spam count
	assert.Equal(t, uint64(10), data["greylisted"])
	assert.Equal(t, uint64(150), data["clean"])
	assert.Equal(t, 20.0, data["spam_percent"])
	assert.Equal(t, "3.4", data["version"])
}

func TestParseRspamdStatEmptyAndInvalid(t *testing.T) {
	data, _, err := ParseRspamdStat([]byte(`{"scanned":0,"actions":{}}`))
	require.NoError(t, err)
	assert.Equal(t, 0.0, data["spam_percent"], "no division by zero on a fresh install")

	_, _, err = ParseRspamdStat([]byte("not json"))
	require.Error(t, err)
}

func TestCollectRspamdStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rspamdStatSample))
	}))
	defer srv.Close()

	data, state, ok := CollectRspamdStats(context.Background(), srv.URL)
	require.True(t, ok)
	assert.Equal(t, "ok", state)
	assert.Equal(t, uint64(200), data["scanned"])
}

func TestCollectRspamdStatsDegradesQuietly(t *testing.T) {
	// Rspamd not installed: the controller port simply does not answer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	url := srv.URL
	srv.Close()

	_, _, ok := CollectRspamdStats(context.Background(), url)
	assert.False(t, ok, "unreachable controller must not be an error")
}
