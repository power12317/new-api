package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ModelChannelConnection struct {
	ChannelID        int      `json:"channel_id"`
	ChannelName      string   `json:"channel_name"`
	ChannelType      int      `json:"channel_type"`
	ChannelStatus    int      `json:"channel_status"`
	ModelName        string   `json:"model_name"`
	Groups           []string `json:"groups"`
	Enabled          bool     `json:"enabled"`
	DirectlyDisabled bool     `json:"directly_disabled"`
}

func GetModelChannelConnections(ctx context.Context, name string, nameRule int) ([]ModelChannelConnection, error) {
	policy, err := LoadModelChannelPolicy(ctx)
	if err != nil {
		return nil, err
	}
	var channels []Channel
	if err := DB.WithContext(ctx).Select("id", "name", "type", "status", "models", "group").Order("id").Find(&channels).Error; err != nil {
		return nil, err
	}
	connections := make([]ModelChannelConnection, 0)
	matcher := Model{ModelName: name, NameRule: nameRule}
	for _, channel := range channels {
		for _, modelName := range normalizeLookupValues(channel.GetModels()) {
			if !matcher.MatchesName(modelName) {
				continue
			}
			direct := slices.ContainsFunc(policy.disabled, func(entry ModelChannelDisable) bool {
				return entry.ChannelID == channel.Id && entry.ModelName == modelName
			})
			connections = append(connections, ModelChannelConnection{
				ChannelID: channel.Id, ChannelName: channel.Name, ChannelType: channel.Type,
				ChannelStatus: channel.Status, ModelName: modelName, Groups: normalizeLookupValues(strings.Split(channel.Group, ",")),
				Enabled: !policy.Blocks(channel.Id, modelName), DirectlyDisabled: direct,
			})
		}
	}
	return connections, nil
}

// ModelChannelDisable persists a model/channel restriction separately from
// generated abilities and imported model metadata. Only disabled pairs exist.
type ModelChannelDisable struct {
	ID        string `json:"-" gorm:"size:64;primaryKey"`
	ChannelID int    `json:"channel_id" gorm:"index;not null"`
	ModelName string `json:"model_name" gorm:"type:text;not null"`
}

type ModelChannelPolicy struct {
	disabled []ModelChannelDisable
	mappings map[int]string
}

// LoadModelChannelPolicy reads the primary database on each new invocation,
// so all nodes and persistent WebSocket sessions observe committed changes.
func LoadModelChannelPolicy(ctx context.Context) (*ModelChannelPolicy, error) {
	if DB == nil {
		return nil, errors.New("model database is unavailable")
	}
	policy := &ModelChannelPolicy{mappings: make(map[int]string)}
	if err := DB.WithContext(ctx).Order("id").Find(&policy.disabled).Error; err != nil {
		return nil, err
	}
	if len(policy.disabled) == 0 {
		return policy, nil
	}
	ids := make([]int, 0, len(policy.disabled))
	for _, entry := range policy.disabled {
		ids = append(ids, entry.ChannelID)
	}
	var channels []Channel
	if err := DB.WithContext(ctx).Select("id", "models", "model_mapping").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	configured := make(map[int][]string, len(channels))
	for _, channel := range channels {
		policy.mappings[channel.Id] = channel.GetModelMapping()
		configured[channel.Id] = normalizeLookupValues(channel.GetModels())
	}
	// A removed model is unavailable, not disabled. Retain its persisted policy
	// for reconfiguration, but do not let that dormant policy block other aliases.
	policy.disabled = slices.DeleteFunc(policy.disabled, func(entry ModelChannelDisable) bool {
		return !slices.Contains(configured[entry.ChannelID], entry.ModelName)
	})
	return policy, nil
}

func (policy *ModelChannelPolicy) Blocks(channelID int, name string) bool {
	var aliases map[string]string
	_ = common.UnmarshalJsonStr(policy.mappings[channelID], &aliases)
	visited := make(map[string]bool)
	for name != "" && !visited[name] {
		visited[name] = true
		base, routing := hostreasoning.BaseModelName(name), ratio_setting.RoutingMatchModelName(name)
		for _, entry := range policy.disabled {
			if entry.ChannelID == channelID && (entry.ModelName == name || entry.ModelName == base || entry.ModelName == routing) {
				return true
			}
		}
		next := aliases[name]
		if next == "" {
			next = aliases[base]
		}
		name = next
	}
	return false
}

func (policy *ModelChannelPolicy) DisabledChannelIDs(name string) []int {
	var ids []int
	for id := range policy.mappings {
		if policy.Blocks(id, name) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (policy *ModelChannelPolicy) FilterModelNames(names []string, connections []ModelConnection, groups []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		blocked, allowed := false, false
		for _, connection := range connections {
			if (connection.Model != name && connection.Model != ratio_setting.RoutingMatchModelName(name)) || (len(groups) > 0 && !slices.Contains(groups, connection.Group)) {
				continue
			}
			if policy.Blocks(connection.ChannelId, name) {
				blocked = true
			} else {
				allowed = true
			}
		}
		if allowed || !blocked {
			result = append(result, name)
		}
	}
	return result
}

func SetModelChannelDisabled(ctx context.Context, channelID int, name string, disabled bool) error {
	if channelID <= 0 || strings.TrimSpace(name) == "" || len(name) > 2048 || strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("Invalid model channel selection")
	}
	hash := sha256.Sum256([]byte(strconv.Itoa(channelID) + "\x00" + name))
	entry := ModelChannelDisable{ID: hex.EncodeToString(hash[:]), ChannelID: channelID, ModelName: name}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !disabled {
			return tx.Where("id = ?", entry.ID).Delete(&ModelChannelDisable{}).Error
		}
		var channel Channel
		if err := lockForUpdate(tx).Select("id", "models").First(&channel, channelID).Error; err != nil {
			return err
		}
		if !slices.Contains(normalizeLookupValues(channel.GetModels()), name) {
			return errors.New("The model is no longer configured on this channel. Reload the model list.")
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry).Error
	})
}
