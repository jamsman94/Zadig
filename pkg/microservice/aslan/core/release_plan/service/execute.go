/*
 * Copyright 2023 The KodeRover Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/koderover/zadig/v2/pkg/microservice/aslan/config"
	"github.com/koderover/zadig/v2/pkg/microservice/aslan/core/common/repository/models"
	"github.com/koderover/zadig/v2/pkg/microservice/aslan/core/common/repository/mongodb"
	"github.com/koderover/zadig/v2/pkg/microservice/aslan/core/workflow/service/workflow"
	jobctl "github.com/koderover/zadig/v2/pkg/microservice/aslan/core/workflow/service/workflow/job"
	"github.com/koderover/zadig/v2/pkg/shared/client/user"
	"github.com/koderover/zadig/v2/pkg/tool/log"
)

type ReleaseJobExecutor interface {
	Execute(plan *models.ReleasePlan) error
}

type ExecuteReleaseJobContext struct {
	AuthResources *user.AuthorizedResources
	UserID        string
	Account       string
	UserName      string
}

func NewReleaseJobExecutor(c *ExecuteReleaseJobContext, args *ExecuteReleaseJobArgs) (ReleaseJobExecutor, error) {
	switch config.ReleasePlanJobType(args.Type) {
	case config.JobText:
		return NewTextReleaseJobExecutor(c, args)
	case config.JobWorkflow:
		return NewWorkflowReleaseJobExecutor(c, args)
	default:
		return nil, errors.Errorf("invalid release job type: %s", args.Type)
	}
}

type TextReleaseJobExecutor struct {
	ID         string
	ExecutedBy string
	Spec       TextReleaseJobSpec
}

type TextReleaseJobSpec struct {
	Remark string `json:"remark"`
}

func NewTextReleaseJobExecutor(c *ExecuteReleaseJobContext, args *ExecuteReleaseJobArgs) (ReleaseJobExecutor, error) {
	var executor TextReleaseJobExecutor
	if err := models.IToi(args.Spec, &executor.Spec); err != nil {
		return nil, errors.Wrap(err, "invalid spec")
	}
	executor.ID = args.ID
	executor.ExecutedBy = c.UserName
	return &executor, nil
}

func (e *TextReleaseJobExecutor) Execute(plan *models.ReleasePlan) error {
	spec := new(models.TextReleaseJobSpec)
	for _, job := range plan.Jobs {
		if job.ID != e.ID {
			continue
		}
		if err := models.IToi(job.Spec, spec); err != nil {
			return errors.Wrap(err, "invalid spec")
		}
		if job.Status != config.ReleasePlanJobStatusTodo {
			return errors.Errorf("job %s status is not todo", job.Name)
		}
		spec.Remark = e.Spec.Remark
		job.Spec = spec
		job.Status = config.ReleasePlanJobStatusDone
		job.ExecutedBy = e.ExecutedBy
		job.ExecutedTime = time.Now().Unix()
		return nil
	}
	return errors.Errorf("job %s not found", e.ID)
}

type WorkflowReleaseJobExecutor struct {
	ID   string
	Ctx  *ExecuteReleaseJobContext
	Spec WorkflowReleaseJobSpec
}

type WorkflowReleaseJobSpec struct {
}

func NewWorkflowReleaseJobExecutor(c *ExecuteReleaseJobContext, args *ExecuteReleaseJobArgs) (ReleaseJobExecutor, error) {
	var executor WorkflowReleaseJobExecutor
	if err := models.IToi(args.Spec, &executor.Spec); err != nil {
		return nil, errors.Wrap(err, "invalid spec")
	}
	executor.ID = args.ID
	executor.Ctx = c
	return &executor, nil
}

func (e *WorkflowReleaseJobExecutor) Execute(plan *models.ReleasePlan) error {
	spec := new(models.WorkflowReleaseJobSpec)
	for _, job := range plan.Jobs {
		if job.ID != e.ID {
			continue
		}
		if err := models.IToi(job.Spec, spec); err != nil {
			return errors.Wrap(err, "invalid spec")
		}
		if spec.Workflow == nil {
			return errors.Errorf("workflow is nil")
		}
		// workflow support retry after failed
		if job.Status != config.ReleasePlanJobStatusTodo && job.Status != config.ReleasePlanJobStatusFailed {
			return errors.Errorf("job %s status %s can't execute", job.Name, job.Status)
		}

		dbStartTime := time.Now().Unix()
		log.Infof(">>>>>>>>>>>>>>> [execute release plan] db query for original workflow started: time: %d <<<<<<<<<<<<<<<<", dbStartTime)
		originalWorkflow, err := mongodb.NewWorkflowV4Coll().Find(spec.Workflow.Name)
		if err != nil {
			log.Errorf("Failed to find WorkflowV4: %s, the error is: %v", spec.Workflow.Name, err)
			return fmt.Errorf("failed to find WorkflowV4: %s, the error is: %v", spec.Workflow.Name, err)
		}
		log.Infof(">>>>>>>>>>>>>>> [execute release plan] db query for original workflow ended: time: %d <<<<<<<<<<<<<<<<", time.Now().Unix())
		log.Infof(">>>>>>>>>>>>>>> [execute release plan] db query for original workflow time used: %d <<<<<<<<<<<<<<<<", time.Now().Unix()-dbStartTime)

		processStartTime := time.Now().Unix()
		log.Infof(">>>>>>>>>>>>>>> [execute release plan] processing workflow execution parameter started: time: %d <<<<<<<<<<<<<<<<", processStartTime)
		if err := jobctl.MergeArgs(originalWorkflow, spec.Workflow); err != nil {
			errMsg := fmt.Sprintf("merge workflow args error: %v", err)
			log.Error(errMsg)
			return fmt.Errorf(errMsg)
		}

		for _, stage := range originalWorkflow.Stages {
			for _, item := range stage.Jobs {
				//err := jobctl.SetOptions(item, originalWorkflow)
				//if err != nil {
				//	errMsg := fmt.Sprintf("merge workflow args set options error: %v", err)
				//	log.Error(errMsg)
				//	return fmt.Errorf(errMsg)
				//}

				// additionally we need to update the user-defined args with the latest workflow configuration
				err = jobctl.UpdateWithLatestSetting(item, originalWorkflow)
				if err != nil {
					errMsg := fmt.Sprintf("failed to merge user-defined workflow args with latest workflow configuration, error: %s", err)
					log.Error(errMsg)
					return fmt.Errorf(errMsg)
				}
			}
		}

		log.Infof(">>>>>>>>>>>>>>> [execute release plan] processing workflow execution parameter ended: time: %d <<<<<<<<<<<<<<<<", time.Now().Unix())
		log.Infof(">>>>>>>>>>>>>>> [execute release plan] processing workflow execution parameter time used: %d <<<<<<<<<<<<<<<<", time.Now().Unix()-processStartTime)

		ctx := e.Ctx
		result, err := workflow.CreateWorkflowTaskV4(&workflow.CreateWorkflowTaskV4Args{
			Name:    ctx.UserName,
			Account: ctx.Account,
			UserID:  ctx.UserID,
		}, originalWorkflow, log.SugaredLogger().With("source", "release plan"))
		if err != nil {
			return errors.Wrapf(err, "failed to create workflow task %s", spec.Workflow.Name)
		}

		spec.TaskID = result.TaskID
		spec.Status = config.StatusPrepare
		job.Spec = spec
		job.Status = config.ReleasePlanJobStatusRunning
		job.ExecutedBy = ctx.UserName
		job.ExecutedTime = time.Now().Unix()
		return nil
	}
	return errors.Errorf("job %s not found", e.ID)
}
