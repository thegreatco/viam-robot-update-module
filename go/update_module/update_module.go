package update_module

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"go.uber.org/zap"
	app_proto "go.viam.com/api/app/v1"
	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	rutils "go.viam.com/rdk/utils"
	"go.viam.com/utils/rpc"
	"google.golang.org/protobuf/types/known/structpb"

	"tennibot-robot-update-module/utils"
)

var Model = resource.NewModel(utils.Namespace, "robot", "update")
var errCredentialsNotFound = errors.New("credentials not found")

func init() {
	resource.RegisterComponent(
		generic.API,
		Model,
		resource.Registration[resource.Resource, *Config]{
			Constructor: NewUpdateModule,
		},
	)
}

func NewUpdateModule(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (resource.Resource, error) {
	logger.Infof("Starting Robot Update Module %v", utils.Version)
	c, cancelFunc := context.WithCancel(context.Background())
	b := RobotUpdateModule{
		Named:      conf.ResourceName().AsNamed(),
		logger:     logger,
		cancelFunc: cancelFunc,
		ctx:        c,
	}

	if err := b.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}
	return &b, nil
}

type RobotUpdateModule struct {
	resource.Named
	logger     logging.Logger
	cancelFunc context.CancelFunc
	ctx        context.Context
}

// Close implements resource.Resource.
func (*RobotUpdateModule) Close(ctx context.Context) error {
	return nil
}

// Reconfigure implements resource.Resource.
func (r *RobotUpdateModule) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	return nil
}

func (b *RobotUpdateModule) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if command, ok := cmd["command"]; ok {
		switch command {
		case "update":
			b.logger.Infof("Received update command")
			if _, ok = cmd["newFragmentId"]; ok {
				newFragmentId, ok := cmd["newFragmentId"].(string)
				if !ok || newFragmentId == "" {
					return map[string]interface{}{"error": "No newFragmentId provided"}, nil
				}
				oldFragmentId, ok := cmd["oldFragmentId"].(string)
				if !ok || oldFragmentId == "" {
					return map[string]interface{}{"error": "No oldFragmentId provided"}, nil
				}

				robotId, ok := cmd["robotId"].(string)
				if !ok || robotId == "" {
					return map[string]interface{}{"error": "No robotId provided"}, nil
				}
				var client app_proto.AppServiceClient
				// If no apiKeyName is provided, use the robot's API key
				if cmd["apiKeyName"] == nil || cmd["apiKey"] == nil {
					cloudId, cloudSecret, err := getCredentialsFromConfig()
					if err != nil {
						return map[string]interface{}{"error": err}, err
					}
					client, err = getAppClientFromConfigCredentials(ctx, b.logger.AsZap(), cloudId, cloudSecret)
					if err != nil {
						b.logger.Errorf("Error getting app client: %v", err)
						return map[string]interface{}{"error": err}, err
					}
				} else {
					apiKeyName, apiKey, err := getApiCredentialsFromRequest(cmd)
					if err != nil {
						return map[string]interface{}{"error": err}, err
					}
					client, err = getAppClientFromApiCredentials(ctx, b.logger.AsZap(), apiKeyName, apiKey)
					if err != nil {
						b.logger.Errorf("Error getting app client: %v", err)
						return map[string]interface{}{"error": err}, err
					}
				}
				return b.updateFragment(ctx, client, robotId, oldFragmentId, newFragmentId)
			} else {
				return map[string]interface{}{"error": "No fragmentId provided"}, nil
			}
		}
	}
	return map[string]interface{}{"error": "No command provided"}, nil
}

func getAppClientFromApiCredentials(ctx context.Context, logger *zap.SugaredLogger, apiKeyName string, apiKey string) (app_proto.AppServiceClient, error) {
	conn, err := rpc.DialDirectGRPC(
		ctx,
		"app.viam.com:443",
		logger,
		rpc.WithEntityCredentials(
			apiKeyName,
			rpc.Credentials{
				Type:    rpc.CredentialsTypeAPIKey,
				Payload: apiKey,
			}),
	)
	if err != nil {
		return nil, err
	}

	return app_proto.NewAppServiceClient(conn), nil
}

func getAppClientFromConfigCredentials(ctx context.Context, logger *zap.SugaredLogger, cloudId, cloudSecret string) (app_proto.AppServiceClient, error) {
	conn, err := rpc.DialDirectGRPC(
		ctx,
		"app.viam.com:443",
		logger,
		rpc.WithEntityCredentials(
			cloudId,
			rpc.Credentials{
				Type:    rutils.CredentialsTypeRobotSecret,
				Payload: cloudSecret,
			}),
	)
	if err != nil {
		return nil, err
	}

	return app_proto.NewAppServiceClient(conn), nil
}

func (b *RobotUpdateModule) updateFragment(ctx context.Context, client app_proto.AppServiceClient, robotId, oldFragmentId, newFragmentId string) (map[string]interface{}, error) {
	b.logger.Infof("Received update fragmentId")

	robot, err := client.GetRobot(ctx, &app_proto.GetRobotRequest{Id: robotId})
	if err != nil {
		b.logger.Errorf("Error getting robot: %v", err)
		return map[string]interface{}{"error": err}, err
	}
	// Does this really provide any value?
	if robot.Robot.LastAccess == nil || robot.Robot.LastAccess.Seconds < time.Now().Unix()-60 {
		b.logger.Errorf("Robot not accessed in the last 60 seconds")
		return map[string]interface{}{"error": "Robot not accessed in the last 60 seconds"}, nil
	}

	parts, err := client.GetRobotParts(ctx, &app_proto.GetRobotPartsRequest{RobotId: robotId})
	if err != nil {
		b.logger.Errorf("Error getting robot parts: %v", err)
		return map[string]interface{}{"error": err}, err
	}

	if parts == nil || len(parts.Parts) == 0 {
		b.logger.Errorf("No parts found for robot: %v", robotId)
		return map[string]interface{}{"error": "No parts found for robot"}, nil
	}

	if len(parts.Parts) > 1 {
		b.logger.Errorf("More than one part found for robot: %v", robotId)
		return map[string]interface{}{"error": "More than one part found for robot"}, nil
	}

	// Get the first part
	part := parts.Parts[0]

	// Get the robot configuration
	conf := part.RobotConfig

	// Create a list of fragments without the old fragment
	newFragments := make([]interface{}, 0)
	if f, ok := conf.Fields["fragments"]; ok {
		fragments := f.GetListValue().Values
		for _, fragment := range fragments {
			b.logger.Debugf("Found fragment: %v", fragment.GetStringValue())
			// Filter out the old fragmentId, we also do the new fragmentId to prevent duplicates, just in case
			if fragment.GetStringValue() != oldFragmentId && fragment.GetStringValue() != newFragmentId {
				b.logger.Debugf("Copying fragment to new fragment list: %v", fragment.GetStringValue())
				newFragments = append(newFragments, fragment)
			}
		}
	}

	// Add the new fragment to the list
	newFragments = append(newFragments, newFragmentId)

	// Go through the fragment_mods and update any overrides that match the old fragment
	if mods, ok := conf.Fields["fragment_mods"]; ok {
		fragmentMods := mods.GetListValue().Values
		for _, fragmentMod := range fragmentMods {
			mod := fragmentMod.GetStructValue()
			if mod.Fields["fragment_id"].GetStringValue() == oldFragmentId {
				b.logger.Infof("Found matching fragment_mod: %v", fragmentMod.GetStringValue())
				// replace the old fragment_id with the new fragment_id
				mod.Fields["fragment_id"] = &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: newFragmentId}}
			}
		}
	}

	// Set the fragments to an array of just the fragmentId
	value, err := structpb.NewList(newFragments)
	if err != nil {
		return nil, err
	}
	conf.Fields["fragments"] = &structpb.Value{Kind: &structpb.Value_ListValue{ListValue: value}}

	// Update the robot part with the new configuration
	_, err = client.UpdateRobotPart(ctx, &app_proto.UpdateRobotPartRequest{Id: part.Id, Name: part.Name, RobotConfig: conf})
	if err != nil {
		b.logger.Errorf("Error updating robot part: %v", err)
		return map[string]interface{}{"error": err}, err
	}
	return map[string]interface{}{"ok": 1}, nil
}

func getApiCredentialsFromRequest(cmd map[string]interface{}) (string, string, error) {
	// First try to get the credentials from the command
	apiKeyName, apiKeyNameOk := cmd["apiKeyName"].(string)
	apiKey, apiKeyOk := cmd["apiKey"].(string)
	if apiKeyOk && apiKeyNameOk {
		return apiKeyName, apiKey, nil
	}
	return "", "", errCredentialsNotFound
}

func getCredentialsFromConfig() (cloudId string, cloudSecret string, err error) {
	var filePath string
	overridePath := os.Getenv("VIAM_CONFIG_FILE")
	if overridePath != "" {
		filePath = overridePath
	} else {
		filePath = "/etc/viam.json"
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", err
	}
	var config ViamConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return "", "", err
	}
	return config.Cloud.ID, config.Cloud.Secret, nil
}

type ViamCloudConfig struct {
	AppAddress string `json:"app_address"`
	ID         string `json:"id"`
	Secret     string `json:"secret"`
}

type ViamConfig struct {
	Cloud ViamCloudConfig `json:"cloud"`
}
