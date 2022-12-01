/*
Copyright 2022 The KodeRover Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package jobcontroller

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/koderover/zadig/pkg/microservice/aslan/config"
	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	kubeclient "github.com/koderover/zadig/pkg/shared/kube/client"
	"github.com/koderover/zadig/pkg/tool/kube/getter"
)

type IstioRollbackJobCtl struct {
	job         *commonmodels.JobTask
	workflowCtx *commonmodels.WorkflowTaskCtx
	logger      *zap.SugaredLogger
	kubeClient  crClient.Client
	jobTaskSpec *commonmodels.JobIstioRollbackSpec
	ack         func()
}

func NewIstioRollbackJobCtl(job *commonmodels.JobTask, workflowCtx *commonmodels.WorkflowTaskCtx, ack func(), logger *zap.SugaredLogger) *IstioRollbackJobCtl {
	jobTaskSpec := &commonmodels.JobIstioRollbackSpec{}
	if err := commonmodels.IToi(job.Spec, jobTaskSpec); err != nil {
		logger.Error(err)
	}

	jobTaskSpec.Replicas = 100

	job.Spec = jobTaskSpec
	return &IstioRollbackJobCtl{
		job:         job,
		workflowCtx: workflowCtx,
		logger:      logger,
		ack:         ack,
		jobTaskSpec: jobTaskSpec,
	}
}

func (c *IstioRollbackJobCtl) Clean(ctx context.Context) {
}

func (c *IstioRollbackJobCtl) Run(ctx context.Context) {
	var err error
	c.kubeClient, err = kubeclient.GetKubeClient(config.HubServerAddress(), c.jobTaskSpec.ClusterID)
	if err != nil {
		logError(c.job, fmt.Sprintf("can't init k8s client: %v", err), c.logger)
		return
	}
	_, found, err := getter.GetDeployment(c.jobTaskSpec.Namespace, c.jobTaskSpec.Service.WorkloadName, c.kubeClient)
	if err != nil || !found {
		logError(c.job, fmt.Sprintf("deployment: %s not found: %v", c.jobTaskSpec.Service.WorkloadName, err), c.logger)
		return
	}

	c.job.Status = config.StatusPassed
}
