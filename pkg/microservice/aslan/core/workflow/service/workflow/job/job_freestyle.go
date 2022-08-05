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

package job

import (
	"fmt"

	configbase "github.com/koderover/zadig/pkg/config"
	"github.com/koderover/zadig/pkg/microservice/aslan/config"
	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	commonrepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb"
	"github.com/koderover/zadig/pkg/tool/log"
	"github.com/koderover/zadig/pkg/types"
	steptypes "github.com/koderover/zadig/pkg/types/step"
)

type FreeStyleJob struct {
	job      *commonmodels.Job
	workflow *commonmodels.WorkflowV4
	spec     *commonmodels.FreestyleJobSpec
}

func (j *FreeStyleJob) Instantiate() error {
	j.spec = &commonmodels.FreestyleJobSpec{}
	if err := commonmodels.IToiYaml(j.job.Spec, j.spec); err != nil {
		return err
	}
	for _, step := range j.spec.Steps {
		switch step.StepType {
		case config.StepTools:
			stepSpec := &steptypes.StepToolInstallSpec{}
			if err := commonmodels.IToiYaml(step.Spec, stepSpec); err != nil {
				return err
			}
			step.Spec = stepSpec
		case config.StepGit:
			stepSpec := &steptypes.StepGitSpec{}
			if err := commonmodels.IToiYaml(step.Spec, stepSpec); err != nil {
				return err
			}
			step.Spec = stepSpec
		case config.StepShell:
			stepSpec := &steptypes.StepShellSpec{}
			if err := commonmodels.IToiYaml(step.Spec, stepSpec); err != nil {
				return err
			}
			step.Spec = stepSpec
		case config.StepArchive:
			stepSpec := &steptypes.StepArchiveSpec{}
			if err := commonmodels.IToiYaml(step.Spec, stepSpec); err != nil {
				return err
			}
			step.Spec = stepSpec
		default:
			return fmt.Errorf("freestyle job step type %s not supported", step.StepType)
		}

	}
	j.job.Spec = j.spec
	return nil
}

func (j *FreeStyleJob) SetPreset() error {
	j.spec = &commonmodels.FreestyleJobSpec{}
	if err := commonmodels.IToi(j.job.Spec, j.spec); err != nil {
		return err
	}
	j.job.Spec = j.spec
	return nil
}

func (j *FreeStyleJob) ToJobs(taskID int64) ([]*commonmodels.JobTask, error) {
	resp := []*commonmodels.JobTask{}
	j.spec = &commonmodels.FreestyleJobSpec{}
	if err := commonmodels.IToi(j.job.Spec, j.spec); err != nil {
		return resp, err
	}
	j.job.Spec = j.spec
	jobTask := &commonmodels.JobTask{
		Name:       j.job.Name,
		JobType:    string(config.JobFreestyle),
		Properties: *j.spec.Properties,
		Steps:      stepsToStepTasks(j.spec.Steps),
	}
	basicImage, err := commonrepo.NewBasicImageColl().Find(jobTask.Properties.ImageID)
	if err != nil {
		return resp, err
	}
	jobTask.Properties.BuildOS = basicImage.Value
	jobTask.Properties.Envs = append(jobTask.Properties.Envs, getfreestyleJobVariables(jobTask.Steps, taskID, j.workflow.Project, j.workflow.Name)...)
	return []*commonmodels.JobTask{jobTask}, nil
}

func stepsToStepTasks(step []*commonmodels.Step) []*commonmodels.StepTask {
	resp := []*commonmodels.StepTask{}
	for _, step := range step {
		stepTask := &commonmodels.StepTask{
			Name:     step.Name,
			StepType: step.StepType,
			Spec:     step.Spec,
		}
		resp = append(resp, stepTask)
	}
	return resp
}

func getfreestyleJobVariables(steps []*commonmodels.StepTask, taskID int64, project, workflowName string) []*commonmodels.KeyVal {
	ret := []*commonmodels.KeyVal{}
	repos := []*types.Repository{}
	for _, step := range steps {
		if step.StepType != config.StepGit {
			continue
		}
		stepSpec := &steptypes.StepGitSpec{}
		if err := commonmodels.IToi(step.Spec, stepSpec); err != nil {
			log.Errorf("failed to convert step spec error: %v", err)
			continue
		}
		repos = append(repos, stepSpec.Repos...)
	}
	ret = append(ret, getReposVariables(repos)...)

	ret = append(ret, &commonmodels.KeyVal{Key: "TASK_ID", Value: fmt.Sprintf("%d", taskID), IsCredential: false})
	buildURL := fmt.Sprintf("%s/v1/projects/detail/%s/pipelines/custom/%s/%d", configbase.SystemAddress(), project, workflowName, taskID)
	ret = append(ret, &commonmodels.KeyVal{Key: "BUILD_URL", Value: buildURL, IsCredential: false})
	return ret
}
