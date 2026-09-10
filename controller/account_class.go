package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ListAccountClasses(c *gin.Context) {
	if rejectRetiredUserRoutingFields(c) {
		return
	}
	classes, err := model.ListAccountClasses(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, classes)
}

func GetOwnAdmissionLimits(c *gin.Context) {
	if rejectRetiredUserRoutingFields(c) {
		return
	}
	limits, err := model.ResolveUserLimits(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, limits)
}

func CreateAccountClass(c *gin.Context) {
	if rejectRetiredUserRoutingFields(c) {
		return
	}
	var a model.AccountClass
	if err := c.ShouldBindJSON(&a); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.CreateAccountClass(c.Request.Context(), a); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"code": a.Code, "policy_version": 1})
}

func UpdateAccountClass(c *gin.Context) {
	if rejectRetiredUserRoutingFields(c) {
		return
	}
	var a model.AccountClass
	if err := c.ShouldBindJSON(&a); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateAccountClass(c.Request.Context(), a); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"code": a.Code, "policy_version": a.PolicyVersion + 1})
}

func DeleteAccountClass(c *gin.Context) {
	if rejectRetiredUserRoutingFields(c) {
		return
	}
	if err := model.DeleteAccountClass(c.Request.Context(), c.Query("code")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"code": c.Query("code")})
}
