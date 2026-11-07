package contracts

import "context"

type ApprovalRequestListInput struct {
	Credential      Credential
	Request         RequestContext
	KnowledgeBaseID ID
	PageSize        int
	Cursor          string
}

type ApprovalRequestPage struct {
	Items      []ApprovalRequest
	NextCursor string
}

type ApprovalRequestListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListApprovalRequests(context.Context, ApprovalRequestListInput) (ApprovalRequestPage, error)
}
