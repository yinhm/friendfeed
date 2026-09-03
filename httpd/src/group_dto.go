package server

import "github.com/yinhm/friendfeed/pb"

type groupFormView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Private     bool   `json:"private,omitempty"`
	Type        string `json:"type,omitempty"`
}

type groupCreatePageData struct {
	Group             groupFormView `json:"group"`
	Error             string        `json:"error,omitempty"`
	PictureAction     string        `json:"picture_action,omitempty"`
	PictureAssetToken string        `json:"picture_asset_token,omitempty"`
}

type groupSettingsPageData struct {
	Group             groupFormView `json:"group"`
	Error             string        `json:"error,omitempty"`
	PictureAction     string        `json:"picture_action,omitempty"`
	PictureAssetToken string        `json:"picture_asset_token,omitempty"`
}

type groupMemberView struct {
	Profile profileView `json:"profile"`
	IsAdmin bool        `json:"is_admin"`
}

type groupMembersPageData struct {
	Group     groupFormView       `json:"group"`
	Members   []groupMemberView   `json:"members"`
	Requests  []followRequestView `json:"requests"`
	HasMore   bool                `json:"has_more"`
	CanManage bool                `json:"can_manage"`
	Error     string              `json:"error,omitempty"`
}

type groupsPageData struct {
	Heading    string          `json:"heading"`
	Groups     []groupFormView `json:"groups"`
	Page       string          `json:"page"`
	EmptyText  string          `json:"empty_text"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func groupFormViewsFromProto(groups []*pb.Profile) []groupFormView {
	views := make([]groupFormView, 0, len(groups))
	for _, group := range groups {
		if group != nil {
			views = append(views, groupFormViewFromProto(group))
		}
	}
	return views
}

func groupMembersPageDataFromProto(group *pb.Profile, members []*pb.GroupMember, requests []*pb.FollowRequestItem, hasMore, canManage bool, errMsg string) groupMembersPageData {
	data := groupMembersPageData{
		Group: groupFormViewFromProto(group), Members: make([]groupMemberView, 0, len(members)),
		Requests: make([]followRequestView, 0, len(requests)), HasMore: hasMore, CanManage: canManage, Error: errMsg,
	}
	for _, member := range members {
		if member != nil && member.Profile != nil {
			data.Members = append(data.Members, groupMemberView{Profile: profileViewFromProto(member.Profile), IsAdmin: member.IsAdmin})
		}
	}
	for _, request := range requests {
		if request != nil && request.Requester != nil {
			data.Requests = append(data.Requests, followRequestView{Requester: profileViewFromProto(request.Requester), RequestedAt: request.RequestedAt})
		}
	}
	return data
}

func groupFormViewFromProto(group *pb.Profile) groupFormView {
	if group == nil {
		return groupFormView{}
	}
	return groupFormView{
		ID:          group.Id,
		Name:        group.Name,
		Description: group.Description,
		Picture:     group.Picture,
		Private:     group.Private,
		Type:        group.Type,
	}
}
