package update_module

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	configutils "github.com/thegreatco/viamutils/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	app_proto "go.viam.com/api/app/v1"
	"go.viam.com/rdk/logging"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestSwapFragmentId(t *testing.T) {
	logger := logging.NewTestLogger(t)

	rawConfigFile, err := os.ReadFile("testdata/robot_part_config.json")
	assert.NoError(t, err)
	part := app_proto.RobotPart{}
	protojson.Unmarshal(rawConfigFile, &part)

	// Get the robot configuration
	conf := part.RobotConfig
	assert.NotNil(t, conf)

	// Swap the fragmentId
	oldFragmentId := "abf95d7c-424a-49f2-b861-9ce999eac2fa"
	newFragmentId := "6abb7bab-769c-4a31-a13b-0f7efa7ab670"
	swapFragmentId(oldFragmentId, newFragmentId, conf, logger)

	updatedConfigBytes, err := protojson.Marshal(&part)
	assert.NoError(t, err)

	expectedConfigBytes, err := os.ReadFile("testdata/robot_part_config_updated.json")
	assert.NoError(t, err)

	expectedConfigJson := map[string]interface{}{}
	err = json.Unmarshal(expectedConfigBytes, &expectedConfigJson)
	assert.NoError(t, err)

	require.JSONEq(t, string(expectedConfigBytes), string(updatedConfigBytes))
}

func TestDoCommandErrors(t *testing.T) {
	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	module := RobotUpdateModule{logger: logger, ctx: ctx}
	command := map[string]interface{}{}
	_, err := module.DoCommand(ctx, command)
	testForExpectedError(t, err, errNoCommandProvided)

	command = map[string]interface{}{
		"command": "update",
	}
	_, err = module.DoCommand(ctx, command)
	testForExpectedError(t, err, errNewFragmentIdMissing)

	command = map[string]interface{}{
		"command":       "update",
		"newFragmentId": "6abb7bab-769c-4a31-a13b-0f7efa7ab670",
	}
	_, err = module.DoCommand(ctx, command)
	testForExpectedError(t, err, errOldFragmentIdMissing)

	command = map[string]interface{}{
		"command":       "update",
		"newFragmentId": "6abb7bab-769c-4a31-a13b-0f7efa7ab670",
		"oldFragmentId": "abf95d7c-424a-49f2-b861-9ce999eac2fa",
	}
	_, err = module.DoCommand(ctx, command)
	testForExpectedError(t, err, errRobotIdMissing)

	command = map[string]interface{}{
		"command":       "update",
		"newFragmentId": "6abb7bab-769c-4a31-a13b-0f7efa7ab670",
		"oldFragmentId": "abf95d7c-424a-49f2-b861-9ce999eac2fa",
		"robotId":       "3bf2974e-59af-409c-bed1-afc1c73d029b",
	}
	_, err = module.DoCommand(ctx, command)
	testForExpectedError(t, err, errCredentialsNotFound)
}

func TestUpdateFragment(t *testing.T) {
	defer func() {
		if _, err := os.Stat("testdata/UpdateRobotPartRequest.json"); err == nil {
			os.Remove("testdata/UpdateRobotPartRequest.json")
		}
	}()
	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	module := RobotUpdateModule{logger: logger, ctx: ctx}

	mockClient := &MockAppServiceClient{}
	_, err := module.updateFragment(ctx, mockClient, "3bf2974e-59af-409c-bed1-afc1c73d029b", "abf95d7c-424a-49f2-b861-9ce999eac2fa", "6abb7bab-769c-4a31-a13b-0f7efa7ab670")
	assert.NoError(t, err)

	expectedConfigBytes, err := os.ReadFile("testdata/UpdateRobotPartRequest_expected.json")
	assert.NoError(t, err)

	updatedConfigBytes, err := os.ReadFile("testdata/UpdateRobotPartRequest.json")
	assert.NoError(t, err)

	require.JSONEq(t, string(expectedConfigBytes), string(updatedConfigBytes))
}

func TestGetApiKeyFromConfig(t *testing.T) {
	cloudId, cloudSecret, err := configutils.GetCredentialsFromConfig()
	assert.Error(t, err, os.ErrNotExist)
	assert.Equal(t, "", cloudId)
	assert.Equal(t, "", cloudSecret)

	os.Setenv("VIAM_CONFIG_FILE", "testdata/viam.json")
	cloudId, cloudSecret, err = configutils.GetCredentialsFromConfig()
	assert.NoError(t, err)
	assert.Equal(t, "cloud_id", cloudId)
	assert.Equal(t, "cloud_secret", cloudSecret)
}

func testForExpectedError(t *testing.T, err error, expectedErr error) {
	assert.Error(t, err)
	assert.Equal(t, err, expectedErr)
}

type MockAppServiceClient struct{}

// GetRobot implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobot(ctx context.Context, in *app_proto.GetRobotRequest, opts ...grpc.CallOption) (*app_proto.GetRobotResponse, error) {
	s, e := os.ReadFile("testdata/GetRobotResponse.json")
	if e != nil {
		return nil, e
	}
	// Update the last accessed date to the current date
	j := strings.ReplaceAll(string(s), "$ONLINEDATE", time.Now().UTC().Format(time.RFC3339))
	s = []byte(j)
	r := &app_proto.GetRobotResponse{}
	e = protojson.Unmarshal(s, r)
	return r, e
}

// GetRobotParts implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobotParts(ctx context.Context, in *app_proto.GetRobotPartsRequest, opts ...grpc.CallOption) (*app_proto.GetRobotPartsResponse, error) {
	s, e := os.ReadFile("testdata/GetRobotPartsResponse.json")
	if e != nil {
		return nil, e
	}
	// Update the last accessed date to the current date
	r := &app_proto.GetRobotPartsResponse{}
	e = protojson.Unmarshal(s, r)
	return r, e
}

// UpdateRobotPart implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateRobotPart(ctx context.Context, in *app_proto.UpdateRobotPartRequest, opts ...grpc.CallOption) (*app_proto.UpdateRobotPartResponse, error) {
	updatedConfigBytes, err := protojson.Marshal(in)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile("testdata/UpdateRobotPartRequest.json", updatedConfigBytes, 0644)
	return &app_proto.UpdateRobotPartResponse{}, err
}

// AddRole implements v1.AppServiceClient.
func (m *MockAppServiceClient) AddRole(ctx context.Context, in *app_proto.AddRoleRequest, opts ...grpc.CallOption) (*app_proto.AddRoleResponse, error) {
	panic("unimplemented")
}

// ChangeRole implements v1.AppServiceClient.
func (m *MockAppServiceClient) ChangeRole(ctx context.Context, in *app_proto.ChangeRoleRequest, opts ...grpc.CallOption) (*app_proto.ChangeRoleResponse, error) {
	panic("unimplemented")
}

// CheckPermissions implements v1.AppServiceClient.
func (m *MockAppServiceClient) CheckPermissions(ctx context.Context, in *app_proto.CheckPermissionsRequest, opts ...grpc.CallOption) (*app_proto.CheckPermissionsResponse, error) {
	panic("unimplemented")
}

// CreateFragment implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateFragment(ctx context.Context, in *app_proto.CreateFragmentRequest, opts ...grpc.CallOption) (*app_proto.CreateFragmentResponse, error) {
	panic("unimplemented")
}

// CreateKey implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateKey(ctx context.Context, in *app_proto.CreateKeyRequest, opts ...grpc.CallOption) (*app_proto.CreateKeyResponse, error) {
	panic("unimplemented")
}

// CreateKeyFromExistingKeyAuthorizations implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateKeyFromExistingKeyAuthorizations(ctx context.Context, in *app_proto.CreateKeyFromExistingKeyAuthorizationsRequest, opts ...grpc.CallOption) (*app_proto.CreateKeyFromExistingKeyAuthorizationsResponse, error) {
	panic("unimplemented")
}

// CreateLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateLocation(ctx context.Context, in *app_proto.CreateLocationRequest, opts ...grpc.CallOption) (*app_proto.CreateLocationResponse, error) {
	panic("unimplemented")
}

// CreateLocationSecret implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateLocationSecret(ctx context.Context, in *app_proto.CreateLocationSecretRequest, opts ...grpc.CallOption) (*app_proto.CreateLocationSecretResponse, error) {
	panic("unimplemented")
}

// CreateModule implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateModule(ctx context.Context, in *app_proto.CreateModuleRequest, opts ...grpc.CallOption) (*app_proto.CreateModuleResponse, error) {
	panic("unimplemented")
}

// CreateOrganization implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateOrganization(ctx context.Context, in *app_proto.CreateOrganizationRequest, opts ...grpc.CallOption) (*app_proto.CreateOrganizationResponse, error) {
	panic("unimplemented")
}

// CreateOrganizationInvite implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateOrganizationInvite(ctx context.Context, in *app_proto.CreateOrganizationInviteRequest, opts ...grpc.CallOption) (*app_proto.CreateOrganizationInviteResponse, error) {
	panic("unimplemented")
}

// CreateRegistryItem implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateRegistryItem(ctx context.Context, in *app_proto.CreateRegistryItemRequest, opts ...grpc.CallOption) (*app_proto.CreateRegistryItemResponse, error) {
	panic("unimplemented")
}

// CreateRobotPartSecret implements v1.AppServiceClient.
func (m *MockAppServiceClient) CreateRobotPartSecret(ctx context.Context, in *app_proto.CreateRobotPartSecretRequest, opts ...grpc.CallOption) (*app_proto.CreateRobotPartSecretResponse, error) {
	panic("unimplemented")
}

// DeleteFragment implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteFragment(ctx context.Context, in *app_proto.DeleteFragmentRequest, opts ...grpc.CallOption) (*app_proto.DeleteFragmentResponse, error) {
	panic("unimplemented")
}

// DeleteKey implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteKey(ctx context.Context, in *app_proto.DeleteKeyRequest, opts ...grpc.CallOption) (*app_proto.DeleteKeyResponse, error) {
	panic("unimplemented")
}

// DeleteLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteLocation(ctx context.Context, in *app_proto.DeleteLocationRequest, opts ...grpc.CallOption) (*app_proto.DeleteLocationResponse, error) {
	panic("unimplemented")
}

// DeleteLocationSecret implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteLocationSecret(ctx context.Context, in *app_proto.DeleteLocationSecretRequest, opts ...grpc.CallOption) (*app_proto.DeleteLocationSecretResponse, error) {
	panic("unimplemented")
}

// DeleteOrganization implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteOrganization(ctx context.Context, in *app_proto.DeleteOrganizationRequest, opts ...grpc.CallOption) (*app_proto.DeleteOrganizationResponse, error) {
	panic("unimplemented")
}

// DeleteOrganizationInvite implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteOrganizationInvite(ctx context.Context, in *app_proto.DeleteOrganizationInviteRequest, opts ...grpc.CallOption) (*app_proto.DeleteOrganizationInviteResponse, error) {
	panic("unimplemented")
}

// DeleteOrganizationMember implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteOrganizationMember(ctx context.Context, in *app_proto.DeleteOrganizationMemberRequest, opts ...grpc.CallOption) (*app_proto.DeleteOrganizationMemberResponse, error) {
	panic("unimplemented")
}

// DeleteRegistryItem implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteRegistryItem(ctx context.Context, in *app_proto.DeleteRegistryItemRequest, opts ...grpc.CallOption) (*app_proto.DeleteRegistryItemResponse, error) {
	panic("unimplemented")
}

// DeleteRobot implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteRobot(ctx context.Context, in *app_proto.DeleteRobotRequest, opts ...grpc.CallOption) (*app_proto.DeleteRobotResponse, error) {
	panic("unimplemented")
}

// DeleteRobotPart implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteRobotPart(ctx context.Context, in *app_proto.DeleteRobotPartRequest, opts ...grpc.CallOption) (*app_proto.DeleteRobotPartResponse, error) {
	panic("unimplemented")
}

// DeleteRobotPartSecret implements v1.AppServiceClient.
func (m *MockAppServiceClient) DeleteRobotPartSecret(ctx context.Context, in *app_proto.DeleteRobotPartSecretRequest, opts ...grpc.CallOption) (*app_proto.DeleteRobotPartSecretResponse, error) {
	panic("unimplemented")
}

// GetFragment implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetFragment(ctx context.Context, in *app_proto.GetFragmentRequest, opts ...grpc.CallOption) (*app_proto.GetFragmentResponse, error) {
	panic("unimplemented")
}

// GetLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetLocation(ctx context.Context, in *app_proto.GetLocationRequest, opts ...grpc.CallOption) (*app_proto.GetLocationResponse, error) {
	panic("unimplemented")
}

// GetModule implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetModule(ctx context.Context, in *app_proto.GetModuleRequest, opts ...grpc.CallOption) (*app_proto.GetModuleResponse, error) {
	panic("unimplemented")
}

// GetOrganization implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetOrganization(ctx context.Context, in *app_proto.GetOrganizationRequest, opts ...grpc.CallOption) (*app_proto.GetOrganizationResponse, error) {
	panic("unimplemented")
}

// GetOrganizationNamespaceAvailability implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetOrganizationNamespaceAvailability(ctx context.Context, in *app_proto.GetOrganizationNamespaceAvailabilityRequest, opts ...grpc.CallOption) (*app_proto.GetOrganizationNamespaceAvailabilityResponse, error) {
	panic("unimplemented")
}

// GetOrganizationsWithAccessToLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetOrganizationsWithAccessToLocation(ctx context.Context, in *app_proto.GetOrganizationsWithAccessToLocationRequest, opts ...grpc.CallOption) (*app_proto.GetOrganizationsWithAccessToLocationResponse, error) {
	panic("unimplemented")
}

// GetRegistryItem implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRegistryItem(ctx context.Context, in *app_proto.GetRegistryItemRequest, opts ...grpc.CallOption) (*app_proto.GetRegistryItemResponse, error) {
	panic("unimplemented")
}

// GetRobotAPIKeys implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobotAPIKeys(ctx context.Context, in *app_proto.GetRobotAPIKeysRequest, opts ...grpc.CallOption) (*app_proto.GetRobotAPIKeysResponse, error) {
	panic("unimplemented")
}

// GetRobotPart implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobotPart(ctx context.Context, in *app_proto.GetRobotPartRequest, opts ...grpc.CallOption) (*app_proto.GetRobotPartResponse, error) {
	panic("unimplemented")
}

// GetRobotPartHistory implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobotPartHistory(ctx context.Context, in *app_proto.GetRobotPartHistoryRequest, opts ...grpc.CallOption) (*app_proto.GetRobotPartHistoryResponse, error) {
	panic("unimplemented")
}

// GetRobotPartLogs implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRobotPartLogs(ctx context.Context, in *app_proto.GetRobotPartLogsRequest, opts ...grpc.CallOption) (*app_proto.GetRobotPartLogsResponse, error) {
	panic("unimplemented")
}

// GetRoverRentalRobots implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetRoverRentalRobots(ctx context.Context, in *app_proto.GetRoverRentalRobotsRequest, opts ...grpc.CallOption) (*app_proto.GetRoverRentalRobotsResponse, error) {
	panic("unimplemented")
}

// GetUserIDByEmail implements v1.AppServiceClient.
func (m *MockAppServiceClient) GetUserIDByEmail(ctx context.Context, in *app_proto.GetUserIDByEmailRequest, opts ...grpc.CallOption) (*app_proto.GetUserIDByEmailResponse, error) {
	panic("unimplemented")
}

// ListAuthorizations implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListAuthorizations(ctx context.Context, in *app_proto.ListAuthorizationsRequest, opts ...grpc.CallOption) (*app_proto.ListAuthorizationsResponse, error) {
	panic("unimplemented")
}

// ListFragments implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListFragments(ctx context.Context, in *app_proto.ListFragmentsRequest, opts ...grpc.CallOption) (*app_proto.ListFragmentsResponse, error) {
	panic("unimplemented")
}

// ListKeys implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListKeys(ctx context.Context, in *app_proto.ListKeysRequest, opts ...grpc.CallOption) (*app_proto.ListKeysResponse, error) {
	panic("unimplemented")
}

// ListLocations implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListLocations(ctx context.Context, in *app_proto.ListLocationsRequest, opts ...grpc.CallOption) (*app_proto.ListLocationsResponse, error) {
	panic("unimplemented")
}

// ListModules implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListModules(ctx context.Context, in *app_proto.ListModulesRequest, opts ...grpc.CallOption) (*app_proto.ListModulesResponse, error) {
	panic("unimplemented")
}

// ListOrganizationMembers implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListOrganizationMembers(ctx context.Context, in *app_proto.ListOrganizationMembersRequest, opts ...grpc.CallOption) (*app_proto.ListOrganizationMembersResponse, error) {
	panic("unimplemented")
}

// ListOrganizations implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListOrganizations(ctx context.Context, in *app_proto.ListOrganizationsRequest, opts ...grpc.CallOption) (*app_proto.ListOrganizationsResponse, error) {
	panic("unimplemented")
}

// ListOrganizationsByUser implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListOrganizationsByUser(ctx context.Context, in *app_proto.ListOrganizationsByUserRequest, opts ...grpc.CallOption) (*app_proto.ListOrganizationsByUserResponse, error) {
	panic("unimplemented")
}

// ListRegistryItems implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListRegistryItems(ctx context.Context, in *app_proto.ListRegistryItemsRequest, opts ...grpc.CallOption) (*app_proto.ListRegistryItemsResponse, error) {
	panic("unimplemented")
}

// ListRobots implements v1.AppServiceClient.
func (m *MockAppServiceClient) ListRobots(ctx context.Context, in *app_proto.ListRobotsRequest, opts ...grpc.CallOption) (*app_proto.ListRobotsResponse, error) {
	panic("unimplemented")
}

// LocationAuth implements v1.AppServiceClient.
func (m *MockAppServiceClient) LocationAuth(ctx context.Context, in *app_proto.LocationAuthRequest, opts ...grpc.CallOption) (*app_proto.LocationAuthResponse, error) {
	panic("unimplemented")
}

// MarkPartAsMain implements v1.AppServiceClient.
func (m *MockAppServiceClient) MarkPartAsMain(ctx context.Context, in *app_proto.MarkPartAsMainRequest, opts ...grpc.CallOption) (*app_proto.MarkPartAsMainResponse, error) {
	panic("unimplemented")
}

// MarkPartForRestart implements v1.AppServiceClient.
func (m *MockAppServiceClient) MarkPartForRestart(ctx context.Context, in *app_proto.MarkPartForRestartRequest, opts ...grpc.CallOption) (*app_proto.MarkPartForRestartResponse, error) {
	panic("unimplemented")
}

// NewRobot implements v1.AppServiceClient.
func (m *MockAppServiceClient) NewRobot(ctx context.Context, in *app_proto.NewRobotRequest, opts ...grpc.CallOption) (*app_proto.NewRobotResponse, error) {
	panic("unimplemented")
}

// NewRobotPart implements v1.AppServiceClient.
func (m *MockAppServiceClient) NewRobotPart(ctx context.Context, in *app_proto.NewRobotPartRequest, opts ...grpc.CallOption) (*app_proto.NewRobotPartResponse, error) {
	panic("unimplemented")
}

// RemoveRole implements v1.AppServiceClient.
func (m *MockAppServiceClient) RemoveRole(ctx context.Context, in *app_proto.RemoveRoleRequest, opts ...grpc.CallOption) (*app_proto.RemoveRoleResponse, error) {
	panic("unimplemented")
}

// ResendOrganizationInvite implements v1.AppServiceClient.
func (m *MockAppServiceClient) ResendOrganizationInvite(ctx context.Context, in *app_proto.ResendOrganizationInviteRequest, opts ...grpc.CallOption) (*app_proto.ResendOrganizationInviteResponse, error) {
	panic("unimplemented")
}

// RotateKey implements v1.AppServiceClient.
func (m *MockAppServiceClient) RotateKey(ctx context.Context, in *app_proto.RotateKeyRequest, opts ...grpc.CallOption) (*app_proto.RotateKeyResponse, error) {
	panic("unimplemented")
}

// ShareLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) ShareLocation(ctx context.Context, in *app_proto.ShareLocationRequest, opts ...grpc.CallOption) (*app_proto.ShareLocationResponse, error) {
	panic("unimplemented")
}

// TailRobotPartLogs implements v1.AppServiceClient.
func (m *MockAppServiceClient) TailRobotPartLogs(ctx context.Context, in *app_proto.TailRobotPartLogsRequest, opts ...grpc.CallOption) (app_proto.AppService_TailRobotPartLogsClient, error) {
	panic("unimplemented")
}

// UnshareLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) UnshareLocation(ctx context.Context, in *app_proto.UnshareLocationRequest, opts ...grpc.CallOption) (*app_proto.UnshareLocationResponse, error) {
	panic("unimplemented")
}

// UpdateFragment implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateFragment(ctx context.Context, in *app_proto.UpdateFragmentRequest, opts ...grpc.CallOption) (*app_proto.UpdateFragmentResponse, error) {
	panic("unimplemented")
}

// UpdateLocation implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateLocation(ctx context.Context, in *app_proto.UpdateLocationRequest, opts ...grpc.CallOption) (*app_proto.UpdateLocationResponse, error) {
	panic("unimplemented")
}

// UpdateModule implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateModule(ctx context.Context, in *app_proto.UpdateModuleRequest, opts ...grpc.CallOption) (*app_proto.UpdateModuleResponse, error) {
	panic("unimplemented")
}

// UpdateOrganization implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateOrganization(ctx context.Context, in *app_proto.UpdateOrganizationRequest, opts ...grpc.CallOption) (*app_proto.UpdateOrganizationResponse, error) {
	panic("unimplemented")
}

// UpdateOrganizationInviteAuthorizations implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateOrganizationInviteAuthorizations(ctx context.Context, in *app_proto.UpdateOrganizationInviteAuthorizationsRequest, opts ...grpc.CallOption) (*app_proto.UpdateOrganizationInviteAuthorizationsResponse, error) {
	panic("unimplemented")
}

// UpdateRegistryItem implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateRegistryItem(ctx context.Context, in *app_proto.UpdateRegistryItemRequest, opts ...grpc.CallOption) (*app_proto.UpdateRegistryItemResponse, error) {
	panic("unimplemented")
}

// UpdateRobot implements v1.AppServiceClient.
func (m *MockAppServiceClient) UpdateRobot(ctx context.Context, in *app_proto.UpdateRobotRequest, opts ...grpc.CallOption) (*app_proto.UpdateRobotResponse, error) {
	panic("unimplemented")
}

// UploadModuleFile implements v1.AppServiceClient.
func (m *MockAppServiceClient) UploadModuleFile(ctx context.Context, opts ...grpc.CallOption) (app_proto.AppService_UploadModuleFileClient, error) {
	panic("unimplemented")
}

func (m *MockAppServiceClient) GetFragmentHistory(ctx context.Context, in *app_proto.GetFragmentHistoryRequest, opts ...grpc.CallOption) (*app_proto.GetFragmentHistoryResponse, error) {
	panic("unimplemented")
}

func (m *MockAppServiceClient) ListMachineFragments(ctx context.Context, in *app_proto.ListMachineFragmentsRequest, opts ...grpc.CallOption) (*app_proto.ListMachineFragmentsResponse, error) {
	panic("unimplemented")
}

func (m *MockAppServiceClient) RenameKey(ctx context.Context, in *app_proto.RenameKeyRequest, opts ...grpc.CallOption) (*app_proto.RenameKeyResponse, error) {
	panic("unimplemented")
}

func (m *MockAppServiceClient) TransferRegistryItem(ctx context.Context, in *app_proto.TransferRegistryItemRequest, opts ...grpc.CallOption) (*app_proto.TransferRegistryItemResponse, error) {
	panic("unimplemented")
}
