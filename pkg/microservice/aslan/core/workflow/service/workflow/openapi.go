package workflow

import (
	"errors"
	"fmt"

	"github.com/koderover/zadig/pkg/microservice/aslan/config"
	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	commonrepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb"
	commonservice "github.com/koderover/zadig/pkg/microservice/aslan/core/common/service"
	jobctl "github.com/koderover/zadig/pkg/microservice/aslan/core/workflow/service/workflow/job"
	"github.com/koderover/zadig/pkg/microservice/systemconfig/core/codehost/repository/mongodb"
	"github.com/koderover/zadig/pkg/setting"
	e "github.com/koderover/zadig/pkg/tool/errors"
	"go.uber.org/zap"
)

// CreateProductWorkflowTask creates a task for product workflow with user-friendly inputs, this is only for
// OpenAPI for now.
func CreateProductWorkflowTask(username string, args *OpenAPICreateProductWorkflowTaskArgs, log *zap.SugaredLogger) (*CreateTaskResp, error) {
	createTaskArgs, err := PresetWorkflowArgs(args.EnvName, args.WorkflowName, log)
	if err != nil {
		presetErr := fmt.Errorf("failed to get the required information to start a workflow, error: %s", err)
		return nil, presetErr
	}

	// buildEnabled means we have build & deploy stage
	if args.BuildEnabled {
		newTargetList := make([]*commonmodels.TargetArgs, 0)
		for _, target := range createTaskArgs.Target {
			for _, svc := range args.ServiceDetail {
				// if the service match
				if svc.ServiceName == target.ServiceName && svc.ServiceModule == target.ImageName {
					for _, repo := range svc.BuildInfo.RepoInfo {
						repoInfo, err := mongodb.NewCodehostColl().GetCodeHostByAlias(repo.CodeHostName)
						if err != nil {
							return nil, errors.New("failed to find code host with name:" + repo.CodeHostName)
						}

						for _, buildRepo := range target.Build.Repos {
							if buildRepo.CodehostID == repoInfo.ID {
								if buildRepo.RepoNamespace == repo.RepoNamespace && buildRepo.RepoName == repo.RepoName {
									buildRepo.Branch = repo.Branch
									buildRepo.PR = repo.PR
								}
							}
						}
					}

					// if deploy is enabled
					if svc.DeployEnabled && svc.DeployInfo.ImageSource == setting.ImageSourceZadig {
						deployInfo := make([]commonmodels.DeployEnv, 0)
						deployInfo = append(deployInfo, commonmodels.DeployEnv{
							Env: fmt.Sprintf("%s/%s", svc.ServiceName, svc.ServiceModule),
							// Fixme: currently using the preset response as value, change it to normal when we have time
							Type:        target.Deploy[0].Type,
							ProductName: target.Deploy[0].ProductName,
						})

						target.Deploy = deployInfo
					}

					newTargetList = append(newTargetList, target)
				}
			}
		}
		createTaskArgs.Target = newTargetList
	} else {
		// otherwise we have a deploy-only stage
		for _, svc := range args.ServiceDetail {

		}
	}

}

// CreateCustomWorkflowTask creates a task for custom workflow with user-friendly inputs, this is currently
// used for openAPI
func CreateCustomWorkflowTask(username string, args *OpenAPICreateCustomWorkflowTaskArgs, log *zap.SugaredLogger) (*CreateTaskV4Resp, error) {
	// first we generate a detailed workflow.
	workflow, err := commonrepo.NewWorkflowV4Coll().Find(args.WorkflowName)
	if err != nil {
		log.Errorf("cannot find workflow %s, the error is: %v", args.WorkflowName, err)
		return nil, e.ErrFindWorkflow.AddDesc(err.Error())
	}

	for _, stage := range workflow.Stages {
		for _, job := range stage.Jobs {
			if err := jobctl.SetPreset(job, workflow); err != nil {
				log.Errorf("cannot get workflow %s preset, the error is: %v", args.WorkflowName, err)
				return nil, e.ErrFindWorkflow.AddDesc(err.Error())
			}
		}
	}

	if err := fillWorkflowV4(workflow, log); err != nil {
		return nil, err
	}

	inputMap := make(map[string]interface{})
	for _, input := range args.Inputs {
		inputMap[input.JobName] = input.Parameters
	}

	for _, stage := range workflow.Stages {
		jobList := make([]*commonmodels.Job, 0)
		for _, job := range stage.Jobs {
			// if a job is found, add it to the job creation list
			if inputParam, ok := inputMap[job.Name]; ok {
				updater, err := getInputUpdater(job, inputParam)
				if err != nil {
					return nil, err
				}
				newJob, err := updater.UpdateJobSpec(job)
				if err != nil {
					log.Errorf("Failed to update jobspec for job: %s, error: %s", job.Name, err)
					return nil, errors.New("failed to update jobspec")
				}
				jobList = append(jobList, newJob)
			}
		}
		stage.Jobs = jobList
	}

	return CreateWorkflowTaskV4(username, workflow, log)
}

func fillWorkflowV4(workflow *commonmodels.WorkflowV4, logger *zap.SugaredLogger) error {
	for _, stage := range workflow.Stages {
		for _, job := range stage.Jobs {
			if job.JobType == config.JobZadigBuild {
				spec := &commonmodels.ZadigBuildJobSpec{}
				if err := commonmodels.IToi(job.Spec, spec); err != nil {
					logger.Errorf(err.Error())
					return e.ErrFindWorkflow.AddErr(err)
				}
				for _, build := range spec.ServiceAndBuilds {
					buildInfo, err := commonrepo.NewBuildColl().Find(&commonrepo.BuildFindOption{Name: build.BuildName})
					if err != nil {
						logger.Errorf(err.Error())
						return e.ErrFindWorkflow.AddErr(err)
					}
					kvs := buildInfo.PreBuild.Envs
					if buildInfo.TemplateID != "" {
						templateEnvs := []*commonmodels.KeyVal{}
						buildTemplate, err := commonrepo.NewBuildTemplateColl().Find(&commonrepo.BuildTemplateQueryOption{
							ID: buildInfo.TemplateID,
						})
						// if template not found, envs are empty, but do not block user.
						if err != nil {
							logger.Error("build job: %s, template not found", buildInfo.Name)
						} else {
							templateEnvs = buildTemplate.PreBuild.Envs
						}

						for _, target := range buildInfo.Targets {
							if target.ServiceName == build.ServiceName && target.ServiceModule == build.ServiceModule {
								kvs = target.Envs
							}
						}
						// if build template update any keyvals, merge it.
						kvs = commonservice.MergeBuildEnvs(templateEnvs, kvs)
					}
					build.KeyVals = commonservice.MergeBuildEnvs(kvs, build.KeyVals)
				}
				job.Spec = spec
			}
			if job.JobType == config.JobFreestyle {
				spec := &commonmodels.FreestyleJobSpec{}
				if err := commonmodels.IToi(job.Spec, spec); err != nil {
					logger.Errorf(err.Error())
					return e.ErrFindWorkflow.AddErr(err)
				}
				job.Spec = spec
			}
			if job.JobType == config.JobPlugin {
				spec := &commonmodels.PluginJobSpec{}
				if err := commonmodels.IToi(job.Spec, spec); err != nil {
					logger.Errorf(err.Error())
					return e.ErrFindWorkflow.AddErr(err)
				}
				job.Spec = spec
			}
		}
	}
	return nil
}

func getInputUpdater(job *commonmodels.Job, input interface{}) (CustomJobInput, error) {
	switch job.JobType {
	case config.JobPlugin:
		updater := new(PluginJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobFreestyle:
		updater := new(FreestyleJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobZadigBuild:
		updater := new(ZadigBuildJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobZadigDeploy:
		updater := new(ZadigDeployJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobK8sBlueGreenDeploy:
		updater := new(BlueGreenDeployJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobK8sCanaryDeploy:
		updater := new(CanaryDeployJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobCustomDeploy:
		updater := new(CustomDeployJobInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	case config.JobK8sBlueGreenRelease, config.JobK8sCanaryRelease:
		updater := new(EmptyInput)
		err := commonmodels.IToi(input, updater)
		return updater, err
	default:
		return nil, errors.New("undefined job type of type:" + string(job.JobType))
	}
}
