package server

import "github.com/yinhm/ffdb/pb"

type requestsPageData struct {
	Requests []followRequestView `json:"requests"`
	Private  bool                `json:"private"`
	Error    string              `json:"error,omitempty"`
}

type followRequestView struct {
	Requester   profileView `json:"requester"`
	RequestedAt string      `json:"requested_at"`
}

func requestsPageDataFromProto(items []*pb.FollowRequestItem, private bool, errMsg string) requestsPageData {
	data := requestsPageData{Requests: make([]followRequestView, 0, len(items)), Private: private, Error: errMsg}
	for _, item := range items {
		if item == nil || item.Requester == nil {
			continue
		}
		data.Requests = append(data.Requests, followRequestView{Requester: profileViewFromProto(item.Requester), RequestedAt: item.RequestedAt})
	}
	return data
}
