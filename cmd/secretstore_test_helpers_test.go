package cmd

import (
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/testutil"
)

func operationKeys(calls []testutil.SecretOperation, method string) []secretstore.Key {
	keys := make([]secretstore.Key, 0, len(calls))
	for _, call := range calls {
		if call.Method == method {
			keys = append(keys, call.Key)
		}
	}
	return keys
}
