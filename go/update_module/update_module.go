package update_module

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	app_proto "go.viam.com/api/app/v1"
	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/robot/client"
	"go.viam.com/utils/rpc"
	"google.golang.org/protobuf/types/known/structpb"

	"viam-robot-update-module/utils"

	api "github.com/thegreatco/viamutils/api"
	configutils "github.com/thegreatco/viamutils/config"
)

var (
	Model                   = resource.NewModel(utils.Namespace, "robot", "update")
	errOldFragmentIdMissing = errors.New("oldFragmentId missing")
	errNewFragmentIdMissing = errors.New("newFragmentId missing")
	errRobotIdMissing       = errors.New("robotId missing")
	errNoCommandProvided    = errors.New("no command provided")
	errRobotNotOnline       = errors.New("robot not online")
	errCredentialsNotFound  = errors.New("credentials not found")
)

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
					return map[string]interface{}{"error": "No newFragmentId provided"}, errNewFragmentIdMissing
				}
				oldFragmentId, ok := cmd["oldFragmentId"].(string)
				if !ok || oldFragmentId == "" {
					return map[string]interface{}{"error": "No oldFragmentId provided"}, errOldFragmentIdMissing
				}
				apiKeyName, apiKey, err := getApiCredentialsFromRequest(cmd)
				if err != nil {
					b.logger.Errorf("Error getting api credentials: %v", err)
					return map[string]interface{}{"error": err}, err
				}
				client, err := b.GetClient(ctx, apiKeyName, apiKey)
				if err != nil {
					b.logger.Errorf("Error getting client: %v", err)
					return map[string]interface{}{"error": err}, err
				}
				machineId, err := configutils.GetMachineId()
				if err != nil {
					return map[string]interface{}{"error": err}, err
				}
				return b.updateFragment(ctx, client, machineId, oldFragmentId, newFragmentId)
			} else {
				return map[string]interface{}{"error": "No fragmentId provided"}, errNewFragmentIdMissing
			}
		case "restart":
			b.logger.Info("received restart request")
			restartViamServer()
			b.logger.Info("sent restart request")
			return map[string]interface{}{"ok": 1}, nil
		case "restart_on_rdk_update":
			b.logger.Info("received restart_on_rdk_update request")
			desiredVersion := cmd["version"].(string)
			if desiredVersion == "" {
				return map[string]interface{}{"error": "no version provided"}, nil
			}
			apiKeyName, apiKey, err := getApiCredentialsFromRequest(cmd)
			if err != nil {
				b.logger.Errorf("Error getting api credentials: %v", err)
				return map[string]interface{}{"error": err}, err
			}
			robotClient, err := b.getRobotClient(ctx, apiKeyName, apiKey)
			if err != nil {
				b.logger.Errorf("Error getting robot client: %v", err)
				return map[string]interface{}{"error": "no robot client"}, nil
			}
			defer robotClient.Close(ctx)
			runningVersion, err := robotClient.Version(ctx)
			if err != nil {
				b.logger.Errorf("Error getting robot version: %v", err)
				return map[string]interface{}{"error": "no robot version"}, nil
			}
			if strings.Contains(runningVersion.Version, desiredVersion) {
				b.logger.Infof("Robot is already running version %s", desiredVersion)
				return map[string]interface{}{"ok": 1, "msg": "viam-server is already on desired version"}, nil
			}

			if v, err := isSymLink("/opt/viam/bin/viam-server"); err == nil && v {
				retryCount := 0
				for {
					if retryCount > 6 {
						b.logger.Errorf("viam-server not updated after 30 seconds")
						return map[string]interface{}{"error": "viam-server not updated after 30 seconds"}, err
					}
					if y, err := isVersion(desiredVersion); err == nil && y {
						break
					}
					time.Sleep(5 * time.Second)
					retryCount++
				}
				restartViamServer()
				b.logger.Infof("viam-server updated and restarted")
				return map[string]interface{}{"ok": 1, "msg": "viam-server updated and restarted"}, nil
			} else if err != nil {
				b.logger.Errorf("Error checking if /opt/viam/bin/viam-server is a symlink: %v", err)
				return map[string]interface{}{"error": "Error checking if /opt/viam/bin/viam-server is a symlink"}, err
			} else {
				b.logger.Infof("/opt/viam/bin/viam-server is not a symlink")
				return map[string]interface{}{"error": "/opt/viam/bin/viam-server is not a symlink"}, err
			}
		}
	}
	return map[string]interface{}{"error": "No command provided"}, errNoCommandProvided
}

func (b *RobotUpdateModule) GetClient(ctx context.Context, apiKeyName, apiKey string) (app_proto.AppServiceClient, error) {
	akn, ak, err := configutils.GetCredentialsFromConfig()
	if err != nil {
		return nil, err
	}
	if apiKeyName != "" && apiKey != "" {
		akn = apiKeyName
		ak = apiKey
	}
	client, err := api.NewAppClientFromApiCredentials(ctx, b.logger, akn, ak)
	return client, err
}

func (b *RobotUpdateModule) getRobotClient(ctx context.Context, apiKeyName, apiKey string) (*client.RobotClient, error) {
	akn, ak, err := configutils.GetCredentialsFromConfig()
	if err != nil {
		return nil, err
	}
	if apiKeyName != "" && apiKey != "" {
		akn = apiKeyName
		ak = apiKey
	}
	config, err := configutils.GetMachineConfig()
	if err != nil {
		return nil, err
	}

	return client.New(
		context.Background(),
		config.Cloud.FQDN,
		b.logger,
		client.WithDialOptions(rpc.WithEntityCredentials(
			akn,
			rpc.Credentials{
				Type:    rpc.CredentialsTypeAPIKey,
				Payload: ak,
			})),
	)
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
		return map[string]interface{}{"error": "Robot not accessed in the last 60 seconds"}, errRobotNotOnline
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

	if conf == nil {
		return map[string]interface{}{"error": "No robot configuration found"}, nil
	}

	// Swap the fragmentId
	swapFragmentId(oldFragmentId, newFragmentId, conf, b.logger)

	// Update the robot part with the new configuration
	_, err = client.UpdateRobotPart(ctx, &app_proto.UpdateRobotPartRequest{Id: part.Id, Name: part.Name, RobotConfig: conf})
	if err != nil {
		b.logger.Errorf("Error updating robot part: %v", err)
		return map[string]interface{}{"error": err}, err
	}
	return map[string]interface{}{"ok": 1}, nil
}

func getApiCredentialsFromRequest(cmd map[string]interface{}) (apiKeyName string, apiKey string, err error) {
	// First try to get the credentials from the command
	apiKeyName, apiKeyNameOk := cmd["apiKeyName"].(string)
	apiKey, apiKeyOk := cmd["apiKey"].(string)
	if apiKeyOk && apiKeyNameOk {
		return apiKeyName, apiKey, nil
	}
	return "", "", errCredentialsNotFound
}

// swapFragmentId swaps the old fragmentId with the new fragmentId in the robot configuration
// This modifies the configuration in place
func swapFragmentId(oldFragmentId, newFragmentId string, conf *structpb.Struct, logger logging.Logger) error {
	// Create a list of fragments without the old fragment
	newFragments := make([]interface{}, 0)
	if f, ok := conf.Fields["fragments"]; ok {
		fragments := f.GetListValue().Values
		for _, fragment := range fragments {
			logger.Debugf("Found fragment: %v", fragment.GetStringValue())
			// Filter out the old fragmentId, we also do the new fragmentId to prevent duplicates, just in case
			if fragment.GetStringValue() != oldFragmentId && fragment.GetStringValue() != newFragmentId {
				logger.Debugf("Copying fragment to new fragment list: %v", fragment.GetStringValue())
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
				logger.Infof("Found matching fragment_mod: %v", fragmentMod.GetStringValue())
				// replace the old fragment_id with the new fragment_id
				mod.Fields["fragment_id"] = &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: newFragmentId}}
			}
		}
	}

	// Set the fragments to an array of just the fragmentId
	value, err := structpb.NewList(newFragments)
	if err != nil {
		return err
	}
	conf.Fields["fragments"] = &structpb.Value{Kind: &structpb.Value_ListValue{ListValue: value}}
	return nil
}

func restartViamServer() error {
	cmd := exec.Command("systemctl", "restart", "viam-agent")
	err := cmd.Run()
	return err
}

func isVersion(version string) (bool, error) {
	fi, err := os.Lstat("/opt/viam/bin/viam-server")
	if err != nil {
		return false, err
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink("/opt/viam/bin/viam-server")
		if err != nil {
			return false, err
		}

		if strings.Contains(link, version) {
			return true, nil
		}
	} else {
		return false, errors.New("viam-server is not a symlink")
	}

	return false, nil
}

func isSymLink(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	return false, nil
}
