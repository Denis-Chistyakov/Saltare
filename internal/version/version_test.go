package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEditionName(t *testing.T) {
	assert.Equal(t, "Open Source", GetEditionName())
}

func TestInfo(t *testing.T) {
	info := Info()
	
	assert.Equal(t, "1.0.0", info["version"])
	assert.Equal(t, "oss", info["edition"])
	assert.Equal(t, "Open Source", info["edition_name"])
}

func TestVersion(t *testing.T) {
	assert.Equal(t, "1.0.0", Version)
	assert.Equal(t, "oss", Edition)
}
