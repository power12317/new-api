package service

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const ModelDisabledCode types.ErrorCode = "model_disabled"
const ModelDisabledMessage = "This model has been disabled. Please use another model."
const ModelChannelDisabledCode types.ErrorCode = "model_channel_disabled"
const ModelChannelDisabledMessage = "This model has been disabled on this channel. Please use another channel."

func FilterInvocableModelNames(ctx context.Context, names, groups []string) ([]string, error) {
	policy, err := model.LoadModelChannelPolicy(ctx)
	if err != nil {
		return nil, err
	}
	connections, err := model.GetModelConnections()
	if err != nil {
		return nil, err
	}
	return policy.FilterModelNames(names, connections, groups), nil
}

func ApplyModelChannelPolicy(c *gin.Context, name string) *ChannelSelectError {
	if name == "" || (c.Request != nil && c.Request.Method == http.MethodGet && c.Request.URL.Path != "/v1/responses" && c.Request.URL.Path != "/v1/realtime") {
		return nil // Existing tasks remain queryable after a model is disabled.
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	policy, err := model.LoadModelChannelPolicy(ctx)
	if err != nil {
		return &ChannelSelectError{StatusCode: http.StatusInternalServerError, Code: types.ErrorCodeQueryDataError, Message: "Failed to check model availability."}
	}
	constraints := GetChannelConstraints(c)
	constraints.Filters = slices.DeleteFunc(constraints.Filters, func(filter dto.ChannelFilter) bool {
		return filter.Kind == dto.FilterModelChannelState
	})
	constraints.AddFilter(dto.ChannelFilter{Kind: dto.FilterModelChannelState, DisabledChannelIDs: policy.DisabledChannelIDs(name)})
	return nil
}

// ModelChannelDenial distinguishes an explicit model restriction from an
// unrelated missing route, group or protocol. Call only after selection fails.
func ModelChannelDenial(c *gin.Context, name, group string) *ChannelSelectError {
	constraints := GetChannelConstraints(c)
	var filters []dto.ChannelFilter
	hasRestrictions := false
	for _, filter := range constraints.Filters {
		if filter.Kind == dto.FilterModelChannelState {
			hasRestrictions = len(filter.DisabledChannelIDs) > 0
			continue
		}
		filters = append(filters, filter)
	}
	if !hasRestrictions {
		return nil
	}
	groups := []string{group}
	if group == "auto" {
		groups = GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	var channels []*model.Channel
	err := model.DB.WithContext(c.Request.Context()).Table("channels").
		Select("channels.id, channels.type, channels.status, channels.setting, channels.settings").Distinct().
		Joins("JOIN abilities ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ? AND abilities.model IN ?", true, common.ChannelStatusEnabled, []string{name, ratio_setting.RoutingMatchModelName(name)}).
		Where(map[string]any{"abilities.group": groups}).Find(&channels).Error
	if err != nil {
		return &ChannelSelectError{StatusCode: http.StatusInternalServerError, Code: types.ErrorCodeQueryDataError, Message: "Failed to check model availability."}
	}
	blocked := false
	for _, channel := range channels {
		if ok, _ := model.ChannelSatisfiesFilters(channel, name, filters); !ok {
			continue
		}
		if ok, _ := model.ChannelSatisfiesFilters(channel, name, constraints.Filters); ok {
			return nil
		}
		blocked = true
	}
	if blocked {
		return &ChannelSelectError{StatusCode: http.StatusForbidden, Code: ModelDisabledCode, Message: ModelDisabledMessage}
	}
	return nil
}

func CheckModelChannelAllowed(ctx context.Context, channelID int, name string) *types.NewAPIError {
	if name == "" {
		return nil
	}
	policy, err := model.LoadModelChannelPolicy(ctx)
	if err != nil {
		return types.NewErrorWithStatusCode(errors.New("Failed to check model availability."), types.ErrorCodeQueryDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	if policy.Blocks(channelID, name) {
		return types.NewErrorWithStatusCode(errors.New(ModelChannelDisabledMessage), ModelChannelDisabledCode, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return nil
}
