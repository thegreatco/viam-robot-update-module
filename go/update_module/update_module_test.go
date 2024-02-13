package update_module

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.viam.com/rdk/logging"
)

func TestUpdateConfig(t *testing.T) {
	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	module := RobotUpdateModule{logger: logger, ctx: ctx}
	command := map[string]interface{}{
		"command": "update",
		"robotId": "58934339-9727-4698-a698-26f5d5e85023",
		// "fragmentId": "6a82b80d-86b6-40cd-bcf8-79c7296dc5d1",
		"fragmentId": "deb4198c-484e-4a05-b06f-4d15a0434044",
		"apiKeyName": "api_key_name",
		"apiKey":     "api_key",
	}
	_, err := module.DoCommand(ctx, command)
	assert.NoError(t, err)
}

func TestMissingApiKey(t *testing.T) {
	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	module := RobotUpdateModule{logger: logger, ctx: ctx}
	command := map[string]interface{}{
		"command": "update",
		"robotId": "58934339-9727-4698-a698-26f5d5e85023",
		// "fragmentId": "6a82b80d-86b6-40cd-bcf8-79c7296dc5d1",
		"fragmentId": "deb4198c-484e-4a05-b06f-4d15a0434044",
		"apiKeyName": "67dd12c6-888e-4a1f-b3c8-c429745f17ff",
	}
	_, err := module.DoCommand(ctx, command)
	assert.Error(t, err, errCredentialsNotFound)
}

func TestMissingApiKeyName(t *testing.T) {
	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	module := RobotUpdateModule{logger: logger, ctx: ctx}
	command := map[string]interface{}{
		"command": "update",
		"robotId": "58934339-9727-4698-a698-26f5d5e85023",
		// "fragmentId": "6a82b80d-86b6-40cd-bcf8-79c7296dc5d1",
		"fragmentId": "deb4198c-484e-4a05-b06f-4d15a0434044",
		"apiKey":     "ucij0ue8g3gzeg7emlnij7arcpwpjqdr",
	}
	_, err := module.DoCommand(ctx, command)
	assert.Error(t, err, errCredentialsNotFound)
}

func TestGetApiKeyFromConfig(t *testing.T) {
	cloudId, cloudSecret, err := getCredentialsFromConfig()
	assert.Error(t, err, os.ErrNotExist)
	assert.Equal(t, "", cloudId)
	assert.Equal(t, "", cloudSecret)

	os.Setenv("VIAM_CONFIG_FILE", "testdata/config.json")
	cloudId, cloudSecret, err = getCredentialsFromConfig()
	assert.NoError(t, err)
	assert.Equal(t, "cloud_id", cloudId)
	assert.Equal(t, "cloud_secret", cloudSecret)
}
