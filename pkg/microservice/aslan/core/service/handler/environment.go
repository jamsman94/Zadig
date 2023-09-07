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

package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/koderover/zadig/pkg/microservice/aslan/core/service/service"
	internalhandler "github.com/koderover/zadig/pkg/shared/handler"
	e "github.com/koderover/zadig/pkg/tool/errors"
)

// @Summary Get Deployable Envs
// @Description Get Deployable Envs
// @Tags 	service
// @Accept 	json
// @Produce json
// @Param 	name			path		string							true	"service name"
// @Param 	projectName		query		string							true	"project name"
// @Success 200 			{object}  	service.DeployableEnvResp
// @Router /api/aslan/service/services/{name}/environments/deployable [get]
func GetDeployableEnvs(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	ctx.Resp, ctx.Err = service.GetDeployableEnvs(c.Param("name"), c.Query("projectName"))
}

// GetKubeWorkloads api used to force user to have get environments privilege to use, now it is removed.
// Revert if necessary
func GetKubeWorkloads(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	ctx.Resp, ctx.Err = service.GetKubeWorkloads(c.Query("namespace"), c.Query("cluster_id"), ctx.Logger)
}

func LoadKubeWorkloadsYaml(c *gin.Context) {
	ctx, err := internalhandler.NewContextWithAuthorization(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	if err != nil {

		ctx.Err = fmt.Errorf("authorization Info Generation failed: err %s", err)
		ctx.UnAuthorized = true
		return
	}

	args := new(service.LoadKubeWorkloadsYamlReq)
	data, err := c.GetRawData()
	if err != nil {
		ctx.Logger.Errorf("copyHelmProduct c.GetRawData() err : %s", err)
	} else if err = json.Unmarshal(data, &args); err != nil {
		ctx.Logger.Errorf("copyHelmProduct json.Unmarshal err : %s", err)
	}
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddErr(err)
		return
	}

	serviceNames := make([]string, 0)
	for _, svc := range args.Services {
		serviceNames = append(serviceNames, svc.Name)
	}

	internalhandler.InsertOperationLog(c, ctx.UserName, args.ProductName, "新增", "项目管理-服务", fmt.Sprintf("服务名称:%s", strings.Join(serviceNames, ",")), string(data), ctx.Logger)

	// authorization checks
	if !ctx.Resources.IsSystemAdmin {
		if _, ok := ctx.Resources.ProjectAuthInfo[args.ProductName]; !ok {
			ctx.UnAuthorized = true
			return
		}
		if !ctx.Resources.ProjectAuthInfo[args.ProductName].IsProjectAdmin &&
			!ctx.Resources.ProjectAuthInfo[args.ProductName].Service.Create {
			ctx.UnAuthorized = true
			return
		}
	}

	ctx.Err = service.LoadKubeWorkloadsYaml(ctx.UserName, args, false, ctx.Logger)
}
