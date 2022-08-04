package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"log"
	"sync"
)

type pageToken struct {
	FilterID uint32
	Offset   int
}

var tokenHasherPool = sync.Pool{
	New: func() interface{} {
		return fnv.New32a()
	},
}

// encodePageToken generates a "token" that represents the current position if a set of results
func encodePageToken(key string, n, offset, page int) string {
	// if we didn't fill the current page then there's no "next" page
	if n < page {
		return ""
	}
	h := tokenHasherPool.Get().(hash.Hash32)
	defer func() {
		h.Reset()
		tokenHasherPool.Put(h)
	}()
	_, _ = h.Write([]byte(key))
	data, _ := json.Marshal(pageToken{
		FilterID: h.Sum32(),
		Offset:   offset + n,
	})
	return base64.StdEncoding.EncodeToString(data)
}

// decodePageToken extracts the position/offset value from the specified token.  If key doesn't match
// the value that was passed to encodePageToken() then this function returns an error.
func decodePageToken(s, key string) (offset int, err error) {
	var data []byte
	if data, err = base64.StdEncoding.DecodeString(s); err != nil {
		return 0, fmt.Errorf("token must be a base64-encoded string: %w", err)
	}
	var tok pageToken
	if err = json.Unmarshal(data, &tok); err != nil {
		// log the JSON error but don't return it to the caller so that the page token can remain opaque
		log.Printf("error (%v) decoding JSON from page token: %q\n", err, string(data))
		return 0, fmt.Errorf("the token contents were invalid")
	}

	h := tokenHasherPool.Get().(hash.Hash32)
	defer func() {
		h.Reset()
		tokenHasherPool.Put(h)
	}()

	_, _ = h.Write([]byte(key))
	if h.Sum32() != tok.FilterID {
		return 0, fmt.Errorf("the provided token was for a different operation")
	}
	return tok.Offset, nil
}
