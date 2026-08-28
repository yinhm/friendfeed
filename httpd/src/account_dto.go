package server

import "github.com/yinhm/ffdb/pb"

type accountPageData struct {
	Tab      string                      `json:"tab"`
	Profile  profileView                 `json:"profile"`
	Services map[string]feedServiceView  `json:"services"`
	States   map[string]serviceStateView `json:"states"`
	Target   string                      `json:"target"`
}

type feedImportPageData struct {
	Feed              feedImportTargetView        `json:"feed"`
	Services          map[string]feedServiceView  `json:"services"`
	States            map[string]serviceStateView `json:"states"`
	Target            string                      `json:"target"`
	ManageServicesURL string                      `json:"manage_services_url"`
	GroupSettingsURL  string                      `json:"group_settings_url,omitempty"`
	GroupMembersURL   string                      `json:"group_members_url,omitempty"`
}

type feedImportTargetView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type profileView struct {
	UUID        string `json:"uuid"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Private     bool   `json:"private,omitempty"`
	Type        string `json:"type,omitempty"`
}

type feedServiceView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	Profile     string `json:"profile,omitempty"`
	Username    string `json:"username,omitempty"`
	Kind        string `json:"kind,omitempty"`
	ServiceUUID string `json:"service_uuid,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type serviceStateView struct {
	LastFetchMS   int64  `json:"last_fetch_ms,omitempty"`
	NextFetchMS   int64  `json:"next_fetch_ms,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	Status        string `json:"status,omitempty"`
	LastSuccessMS int64  `json:"last_success_ms,omitempty"`
}

func profileViewFromProto(profile *pb.Profile) profileView {
	if profile == nil {
		return profileView{}
	}
	return profileView{UUID: profile.Uuid, ID: profile.Id, Name: profile.Name, Description: profile.Description, Picture: profile.Picture, Private: profile.Private, Type: profile.Type}
}

func accountPageDataFromProto(tab, target string, profile *pb.Profile, response *pb.ListFeedServicesResponse) accountPageData {
	services, states := feedServiceViewsFromProto(response)
	return accountPageData{Tab: tab, Profile: profileViewFromProto(profile), Services: services, States: states, Target: target}
}

func feedServiceViewsFromProto(response *pb.ListFeedServicesResponse) (map[string]feedServiceView, map[string]serviceStateView) {
	services := map[string]feedServiceView{}
	states := map[string]serviceStateView{}
	if response == nil {
		return services, states
	}
	for _, service := range response.Services {
		if service == nil {
			continue
		}
		services[service.Id] = feedServiceViewFromProto(service)
	}
	for id, state := range response.States {
		if state == nil {
			continue
		}
		states[id] = serviceStateView{LastFetchMS: state.LastFetchMs, NextFetchMS: state.NextFetchMs, LastError: state.LastError, Status: state.Status, LastSuccessMS: state.LastSuccessMs}
	}
	return services, states
}

func feedServiceViewFromProto(service *pb.FeedService) feedServiceView {
	if service == nil {
		return feedServiceView{}
	}
	return feedServiceView{ID: service.Id, Name: service.Name, Icon: service.Icon, Profile: service.Profile, Username: service.Username, Kind: service.Kind, ServiceUUID: service.ServiceUuid, Enabled: service.Enabled}
}
