package server

import "github.com/yinhm/ffdb/pb"

type groupFormView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

type groupCreatePageData struct {
	Group         groupFormView `json:"group"`
	CurrentUserID string        `json:"current_user_id"`
	Error         string        `json:"error,omitempty"`
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
	}
}
