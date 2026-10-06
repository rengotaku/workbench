package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rengotaku/note-stock/internal/testutil"
)

func TestNewTestDB(t *testing.T) {
	db := testutil.NewTestDB(t)
	assert.NotNil(t, db)
}
