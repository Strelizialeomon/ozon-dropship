// 面单两步流程（官方旧接口 /v2/posting/fbs/package-label 于 2026-11-02 关停后的替代）：
//
//	CreatePackageLabel（/v3/posting/fbs/package-label/create）→ 拿 task_id
//	GetPackageLabelTask（/v2/posting/fbs/package-label/get）→ 轮询拿 file_url
//
// 异步等待由调用方（S1-D）负责：官方建议组装后 45–60 秒再取；
// status.code 到 completed 前重复调 GetPackageLabelTask（注意经限流器排队）。
// file_url 的下载方式（是否需认证头）待真实店实测。
package ozon

import (
	"context"
	"fmt"
)

// 面单任务类型（官方 task_type）。
const (
	LabelTaskTypeBig   = "big_label"   // 普通面单
	LabelTaskTypeSmall = "small_label" // 小面单
)

// 面单任务状态码（官方 status.code）。
const (
	LabelTaskPending    = "pending"     // 排队中
	LabelTaskInProgress = "in_progress" // 生成中
	LabelTaskCompleted  = "completed"   // 文件就绪（file_url 可用）
	LabelTaskError      = "error"       // 出错
)

// LabelTask 一个面单生成任务（create 响应 tasks[] 的元素）。
type LabelTask struct {
	TaskID   int64  `json:"task_id"`
	TaskType string `json:"task_type"`
}

// UnprintedPosting 未能生成面单的寄件（status.unprinted_postings[] 的元素）。
type UnprintedPosting struct {
	Message       string `json:"message"`
	PostingNumber string `json:"posting_number"`
}

// LabelTaskStatus 任务状态（官方 status）。
type LabelTaskStatus struct {
	// Code 任务状态：LabelTaskPending / InProgress / Completed / Error。
	Code string `json:"code"`
	// PostingsCount 请求面单的寄件数。
	PostingsCount int32 `json:"postings_count"`
	// PrintedPostingsCount 成功生成面单的寄件数。
	PrintedPostingsCount int32 `json:"printed_postings_count"`
	// UnprintedPostings 失败明细。
	UnprintedPostings []UnprintedPosting `json:"unprinted_postings"`
}

// LabelTaskErrorInfo 任务级错误（官方 error；与 status.code=error 搭配）。
type LabelTaskErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LabelTaskResult 任务结果（get 响应）。
type LabelTaskResult struct {
	// FileURL 面单文件地址（status.code=completed 后可用）。
	FileURL string             `json:"file_url"`
	Status  *LabelTaskStatus   `json:"status"`
	Error   *LabelTaskErrorInfo `json:"error"`
}

type labelTaskCreateRequest struct {
	PostingNumbers []string `json:"posting_numbers"`
}

// CreatePackageLabel 创建面单生成任务（/v3/posting/fbs/package-label/create）。
// 返回每个寄件集对应的任务（含 task_id，后续用 GetPackageLabelTask 取文件）。
func (c *Client) CreatePackageLabel(ctx context.Context, postingNumbers []string) ([]LabelTask, error) {
	if len(postingNumbers) == 0 {
		return nil, fmt.Errorf("%w: CreatePackageLabel 需要至少一个寄件号", ErrInvalidParams)
	}
	var resp struct {
		Tasks []LabelTask `json:"tasks"`
	}
	req := labelTaskCreateRequest{PostingNumbers: postingNumbers}
	if err := c.do(ctx, EndpointLabelTaskCreate, pathLabelTaskCreate, req, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

type labelTaskGetRequest struct {
	TaskID int64 `json:"task_id"`
}

// GetPackageLabelTask 查询面单任务（/v2/posting/fbs/package-label/get）。
// status.code 到 LabelTaskCompleted 时 FileURL 可用；未完成时由调用方按需重试。
func (c *Client) GetPackageLabelTask(ctx context.Context, taskID int64) (*LabelTaskResult, error) {
	if taskID <= 0 {
		return nil, fmt.Errorf("%w: GetPackageLabelTask 需要正的 taskID", ErrInvalidParams)
	}
	var resp LabelTaskResult
	req := labelTaskGetRequest{TaskID: taskID}
	if err := c.do(ctx, EndpointLabelTaskGet, pathLabelTaskGet, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
