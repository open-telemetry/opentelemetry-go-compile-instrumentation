// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Code generated from github.com/linode/linodego/v2@v2.4.1. DO NOT EDIT.
// Unique Before/After wrappers per public Client method so otelc can emit
// one //go:linkname stub per name without package-level redeclarations.
//
// Regenerate with: go run ./gen -version v2.4.1

package v2

import "go.opentelemetry.io/otelc/pkg/hook"

// BeforeAcceptAccountServiceTransfer instruments (*Client).AcceptAccountServiceTransfer.
func BeforeAcceptAccountServiceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterAcceptAccountServiceTransfer finishes the span for (*Client).AcceptAccountServiceTransfer.
func AfterAcceptAccountServiceTransfer(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeAcknowledgeAccountAgreements instruments (*Client).AcknowledgeAccountAgreements.
func BeforeAcknowledgeAccountAgreements(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterAcknowledgeAccountAgreements finishes the span for (*Client).AcknowledgeAccountAgreements.
func AfterAcknowledgeAccountAgreements(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeAddInstanceIPAddress instruments (*Client).AddInstanceIPAddress.
func BeforeAddInstanceIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterAddInstanceIPAddress finishes the span for (*Client).AddInstanceIPAddress.
func AfterAddInstanceIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAddPaymentMethod instruments (*Client).AddPaymentMethod.
func BeforeAddPaymentMethod(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterAddPaymentMethod finishes the span for (*Client).AddPaymentMethod.
func AfterAddPaymentMethod(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeAddPromoCode instruments (*Client).AddPromoCode.
func BeforeAddPromoCode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterAddPromoCode finishes the span for (*Client).AddPromoCode.
func AfterAddPromoCode(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAllocateReserveIP instruments (*Client).AllocateReserveIP.
func BeforeAllocateReserveIP(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterAllocateReserveIP finishes the span for (*Client).AllocateReserveIP.
func AfterAllocateReserveIP(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAppendInstanceConfigInterface instruments (*Client).AppendInstanceConfigInterface.
func BeforeAppendInstanceConfigInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterAppendInstanceConfigInterface finishes the span for (*Client).AppendInstanceConfigInterface.
func AfterAppendInstanceConfigInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAssignInstanceReservedIP instruments (*Client).AssignInstanceReservedIP.
func BeforeAssignInstanceReservedIP(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterAssignInstanceReservedIP finishes the span for (*Client).AssignInstanceReservedIP.
func AfterAssignInstanceReservedIP(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAssignPlacementGroupLinodes instruments (*Client).AssignPlacementGroupLinodes.
func BeforeAssignPlacementGroupLinodes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterAssignPlacementGroupLinodes finishes the span for (*Client).AssignPlacementGroupLinodes.
func AfterAssignPlacementGroupLinodes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeAttachVolume instruments (*Client).AttachVolume.
func BeforeAttachVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterAttachVolume finishes the span for (*Client).AttachVolume.
func AfterAttachVolume(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeBootInstance instruments (*Client).BootInstance.
func BeforeBootInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterBootInstance finishes the span for (*Client).BootInstance.
func AfterBootInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeCancelAccountServiceTransfer instruments (*Client).CancelAccountServiceTransfer.
func BeforeCancelAccountServiceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCancelAccountServiceTransfer finishes the span for (*Client).CancelAccountServiceTransfer.
func AfterCancelAccountServiceTransfer(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeCancelInstanceBackups instruments (*Client).CancelInstanceBackups.
func BeforeCancelInstanceBackups(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCancelInstanceBackups finishes the span for (*Client).CancelInstanceBackups.
func AfterCancelInstanceBackups(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeCancelObjectStorage instruments (*Client).CancelObjectStorage.
func BeforeCancelObjectStorage(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterCancelObjectStorage finishes the span for (*Client).CancelObjectStorage.
func AfterCancelObjectStorage(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeCloneDomain instruments (*Client).CloneDomain.
func BeforeCloneDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCloneDomain finishes the span for (*Client).CloneDomain.
func AfterCloneDomain(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCloneInstance instruments (*Client).CloneInstance.
func BeforeCloneInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCloneInstance finishes the span for (*Client).CloneInstance.
func AfterCloneInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCloneInstanceDisk instruments (*Client).CloneInstanceDisk.
func BeforeCloneInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCloneInstanceDisk finishes the span for (*Client).CloneInstanceDisk.
func AfterCloneInstanceDisk(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCloneMonitorAlertDefinition instruments (*Client).CloneMonitorAlertDefinition.
func BeforeCloneMonitorAlertDefinition(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterCloneMonitorAlertDefinition finishes the span for (*Client).CloneMonitorAlertDefinition.
func AfterCloneMonitorAlertDefinition(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCloneVolume instruments (*Client).CloneVolume.
func BeforeCloneVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCloneVolume finishes the span for (*Client).CloneVolume.
func AfterCloneVolume(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeConfirmTwoFactor instruments (*Client).ConfirmTwoFactor.
func BeforeConfirmTwoFactor(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterConfirmTwoFactor finishes the span for (*Client).ConfirmTwoFactor.
func AfterConfirmTwoFactor(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateChildAccountToken instruments (*Client).CreateChildAccountToken.
func BeforeCreateChildAccountToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateChildAccountToken finishes the span for (*Client).CreateChildAccountToken.
func AfterCreateChildAccountToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateDomain instruments (*Client).CreateDomain.
func BeforeCreateDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateDomain finishes the span for (*Client).CreateDomain.
func AfterCreateDomain(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateDomainRecord instruments (*Client).CreateDomainRecord.
func BeforeCreateDomainRecord(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateDomainRecord finishes the span for (*Client).CreateDomainRecord.
func AfterCreateDomainRecord(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateFirewall instruments (*Client).CreateFirewall.
func BeforeCreateFirewall(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateFirewall finishes the span for (*Client).CreateFirewall.
func AfterCreateFirewall(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateFirewallDevice instruments (*Client).CreateFirewallDevice.
func BeforeCreateFirewallDevice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateFirewallDevice finishes the span for (*Client).CreateFirewallDevice.
func AfterCreateFirewallDevice(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateFirewallRuleSet instruments (*Client).CreateFirewallRuleSet.
func BeforeCreateFirewallRuleSet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateFirewallRuleSet finishes the span for (*Client).CreateFirewallRuleSet.
func AfterCreateFirewallRuleSet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateIPv6Range instruments (*Client).CreateIPv6Range.
func BeforeCreateIPv6Range(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateIPv6Range finishes the span for (*Client).CreateIPv6Range.
func AfterCreateIPv6Range(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateImage instruments (*Client).CreateImage.
func BeforeCreateImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateImage finishes the span for (*Client).CreateImage.
func AfterCreateImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateImageShareGroup instruments (*Client).CreateImageShareGroup.
func BeforeCreateImageShareGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateImageShareGroup finishes the span for (*Client).CreateImageShareGroup.
func AfterCreateImageShareGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateImageUpload instruments (*Client).CreateImageUpload.
func BeforeCreateImageUpload(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateImageUpload finishes the span for (*Client).CreateImageUpload.
func AfterCreateImageUpload(ictx hook.HookContext, r0 interface{}, r1 interface{}, r2 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1, r2))
}

// BeforeCreateInstance instruments (*Client).CreateInstance.
func BeforeCreateInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateInstance finishes the span for (*Client).CreateInstance.
func AfterCreateInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateInstanceConfig instruments (*Client).CreateInstanceConfig.
func BeforeCreateInstanceConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateInstanceConfig finishes the span for (*Client).CreateInstanceConfig.
func AfterCreateInstanceConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateInstanceDisk instruments (*Client).CreateInstanceDisk.
func BeforeCreateInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateInstanceDisk finishes the span for (*Client).CreateInstanceDisk.
func AfterCreateInstanceDisk(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateInstanceSnapshot instruments (*Client).CreateInstanceSnapshot.
func BeforeCreateInstanceSnapshot(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateInstanceSnapshot finishes the span for (*Client).CreateInstanceSnapshot.
func AfterCreateInstanceSnapshot(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateInterface instruments (*Client).CreateInterface.
func BeforeCreateInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateInterface finishes the span for (*Client).CreateInterface.
func AfterCreateInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLKECluster instruments (*Client).CreateLKECluster.
func BeforeCreateLKECluster(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateLKECluster finishes the span for (*Client).CreateLKECluster.
func AfterCreateLKECluster(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLKENodePool instruments (*Client).CreateLKENodePool.
func BeforeCreateLKENodePool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateLKENodePool finishes the span for (*Client).CreateLKENodePool.
func AfterCreateLKENodePool(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLock instruments (*Client).CreateLock.
func BeforeCreateLock(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateLock finishes the span for (*Client).CreateLock.
func AfterCreateLock(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLogStream instruments (*Client).CreateLogStream.
func BeforeCreateLogStream(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateLogStream finishes the span for (*Client).CreateLogStream.
func AfterCreateLogStream(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLogsDestination instruments (*Client).CreateLogsDestination.
func BeforeCreateLogsDestination(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateLogsDestination finishes the span for (*Client).CreateLogsDestination.
func AfterCreateLogsDestination(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateLongviewClient instruments (*Client).CreateLongviewClient.
func BeforeCreateLongviewClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateLongviewClient finishes the span for (*Client).CreateLongviewClient.
func AfterCreateLongviewClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateMonitorAlertDefinition instruments (*Client).CreateMonitorAlertDefinition.
func BeforeCreateMonitorAlertDefinition(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateMonitorAlertDefinition finishes the span for (*Client).CreateMonitorAlertDefinition.
func AfterCreateMonitorAlertDefinition(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateMonitorAlertDefinitionWithIdempotency instruments (*Client).CreateMonitorAlertDefinitionWithIdempotency.
func BeforeCreateMonitorAlertDefinitionWithIdempotency(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterCreateMonitorAlertDefinitionWithIdempotency finishes the span for (*Client).CreateMonitorAlertDefinitionWithIdempotency.
func AfterCreateMonitorAlertDefinitionWithIdempotency(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateMonitorServiceTokenForServiceType instruments (*Client).CreateMonitorServiceTokenForServiceType.
func BeforeCreateMonitorServiceTokenForServiceType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateMonitorServiceTokenForServiceType finishes the span for (*Client).CreateMonitorServiceTokenForServiceType.
func AfterCreateMonitorServiceTokenForServiceType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateMySQLDatabase instruments (*Client).CreateMySQLDatabase.
func BeforeCreateMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateMySQLDatabase finishes the span for (*Client).CreateMySQLDatabase.
func AfterCreateMySQLDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateNodeBalancer instruments (*Client).CreateNodeBalancer.
func BeforeCreateNodeBalancer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateNodeBalancer finishes the span for (*Client).CreateNodeBalancer.
func AfterCreateNodeBalancer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateNodeBalancerConfig instruments (*Client).CreateNodeBalancerConfig.
func BeforeCreateNodeBalancerConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateNodeBalancerConfig finishes the span for (*Client).CreateNodeBalancerConfig.
func AfterCreateNodeBalancerConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateNodeBalancerNode instruments (*Client).CreateNodeBalancerNode.
func BeforeCreateNodeBalancerNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterCreateNodeBalancerNode finishes the span for (*Client).CreateNodeBalancerNode.
func AfterCreateNodeBalancerNode(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateOAuthClient instruments (*Client).CreateOAuthClient.
func BeforeCreateOAuthClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateOAuthClient finishes the span for (*Client).CreateOAuthClient.
func AfterCreateOAuthClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateObjectStorageBucket instruments (*Client).CreateObjectStorageBucket.
func BeforeCreateObjectStorageBucket(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateObjectStorageBucket finishes the span for (*Client).CreateObjectStorageBucket.
func AfterCreateObjectStorageBucket(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateObjectStorageKey instruments (*Client).CreateObjectStorageKey.
func BeforeCreateObjectStorageKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateObjectStorageKey finishes the span for (*Client).CreateObjectStorageKey.
func AfterCreateObjectStorageKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateObjectStorageObjectURL instruments (*Client).CreateObjectStorageObjectURL.
func BeforeCreateObjectStorageObjectURL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterCreateObjectStorageObjectURL finishes the span for (*Client).CreateObjectStorageObjectURL.
func AfterCreateObjectStorageObjectURL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreatePayment instruments (*Client).CreatePayment.
func BeforeCreatePayment(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreatePayment finishes the span for (*Client).CreatePayment.
func AfterCreatePayment(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreatePlacementGroup instruments (*Client).CreatePlacementGroup.
func BeforeCreatePlacementGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreatePlacementGroup finishes the span for (*Client).CreatePlacementGroup.
func AfterCreatePlacementGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreatePostgresDatabase instruments (*Client).CreatePostgresDatabase.
func BeforeCreatePostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreatePostgresDatabase finishes the span for (*Client).CreatePostgresDatabase.
func AfterCreatePostgresDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateSSHKey instruments (*Client).CreateSSHKey.
func BeforeCreateSSHKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateSSHKey finishes the span for (*Client).CreateSSHKey.
func AfterCreateSSHKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateStackscript instruments (*Client).CreateStackscript.
func BeforeCreateStackscript(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateStackscript finishes the span for (*Client).CreateStackscript.
func AfterCreateStackscript(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateTag instruments (*Client).CreateTag.
func BeforeCreateTag(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateTag finishes the span for (*Client).CreateTag.
func AfterCreateTag(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateToken instruments (*Client).CreateToken.
func BeforeCreateToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateToken finishes the span for (*Client).CreateToken.
func AfterCreateToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateTwoFactorSecret instruments (*Client).CreateTwoFactorSecret.
func BeforeCreateTwoFactorSecret(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterCreateTwoFactorSecret finishes the span for (*Client).CreateTwoFactorSecret.
func AfterCreateTwoFactorSecret(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateUser instruments (*Client).CreateUser.
func BeforeCreateUser(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateUser finishes the span for (*Client).CreateUser.
func AfterCreateUser(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateVPC instruments (*Client).CreateVPC.
func BeforeCreateVPC(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateVPC finishes the span for (*Client).CreateVPC.
func AfterCreateVPC(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateVPCSubnet instruments (*Client).CreateVPCSubnet.
func BeforeCreateVPCSubnet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterCreateVPCSubnet finishes the span for (*Client).CreateVPCSubnet.
func AfterCreateVPCSubnet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeCreateVolume instruments (*Client).CreateVolume.
func BeforeCreateVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterCreateVolume finishes the span for (*Client).CreateVolume.
func AfterCreateVolume(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeDeleteDomain instruments (*Client).DeleteDomain.
func BeforeDeleteDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteDomain finishes the span for (*Client).DeleteDomain.
func AfterDeleteDomain(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteDomainRecord instruments (*Client).DeleteDomainRecord.
func BeforeDeleteDomainRecord(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteDomainRecord finishes the span for (*Client).DeleteDomainRecord.
func AfterDeleteDomainRecord(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteFirewall instruments (*Client).DeleteFirewall.
func BeforeDeleteFirewall(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteFirewall finishes the span for (*Client).DeleteFirewall.
func AfterDeleteFirewall(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteFirewallDevice instruments (*Client).DeleteFirewallDevice.
func BeforeDeleteFirewallDevice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteFirewallDevice finishes the span for (*Client).DeleteFirewallDevice.
func AfterDeleteFirewallDevice(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteFirewallRuleSet instruments (*Client).DeleteFirewallRuleSet.
func BeforeDeleteFirewallRuleSet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteFirewallRuleSet finishes the span for (*Client).DeleteFirewallRuleSet.
func AfterDeleteFirewallRuleSet(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteIPv6Range instruments (*Client).DeleteIPv6Range.
func BeforeDeleteIPv6Range(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteIPv6Range finishes the span for (*Client).DeleteIPv6Range.
func AfterDeleteIPv6Range(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteImage instruments (*Client).DeleteImage.
func BeforeDeleteImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteImage finishes the span for (*Client).DeleteImage.
func AfterDeleteImage(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteImageShareGroup instruments (*Client).DeleteImageShareGroup.
func BeforeDeleteImageShareGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteImageShareGroup finishes the span for (*Client).DeleteImageShareGroup.
func AfterDeleteImageShareGroup(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInstance instruments (*Client).DeleteInstance.
func BeforeDeleteInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteInstance finishes the span for (*Client).DeleteInstance.
func AfterDeleteInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInstanceConfig instruments (*Client).DeleteInstanceConfig.
func BeforeDeleteInstanceConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteInstanceConfig finishes the span for (*Client).DeleteInstanceConfig.
func AfterDeleteInstanceConfig(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInstanceConfigInterface instruments (*Client).DeleteInstanceConfigInterface.
func BeforeDeleteInstanceConfigInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterDeleteInstanceConfigInterface finishes the span for (*Client).DeleteInstanceConfigInterface.
func AfterDeleteInstanceConfigInterface(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInstanceDisk instruments (*Client).DeleteInstanceDisk.
func BeforeDeleteInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteInstanceDisk finishes the span for (*Client).DeleteInstanceDisk.
func AfterDeleteInstanceDisk(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInstanceIPAddress instruments (*Client).DeleteInstanceIPAddress.
func BeforeDeleteInstanceIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteInstanceIPAddress finishes the span for (*Client).DeleteInstanceIPAddress.
func AfterDeleteInstanceIPAddress(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteInterface instruments (*Client).DeleteInterface.
func BeforeDeleteInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteInterface finishes the span for (*Client).DeleteInterface.
func AfterDeleteInterface(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKECluster instruments (*Client).DeleteLKECluster.
func BeforeDeleteLKECluster(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLKECluster finishes the span for (*Client).DeleteLKECluster.
func AfterDeleteLKECluster(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKEClusterControlPlaneACL instruments (*Client).DeleteLKEClusterControlPlaneACL.
func BeforeDeleteLKEClusterControlPlaneACL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLKEClusterControlPlaneACL finishes the span for (*Client).DeleteLKEClusterControlPlaneACL.
func AfterDeleteLKEClusterControlPlaneACL(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKEClusterKubeconfig instruments (*Client).DeleteLKEClusterKubeconfig.
func BeforeDeleteLKEClusterKubeconfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLKEClusterKubeconfig finishes the span for (*Client).DeleteLKEClusterKubeconfig.
func AfterDeleteLKEClusterKubeconfig(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKEClusterServiceToken instruments (*Client).DeleteLKEClusterServiceToken.
func BeforeDeleteLKEClusterServiceToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLKEClusterServiceToken finishes the span for (*Client).DeleteLKEClusterServiceToken.
func AfterDeleteLKEClusterServiceToken(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKENodePool instruments (*Client).DeleteLKENodePool.
func BeforeDeleteLKENodePool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteLKENodePool finishes the span for (*Client).DeleteLKENodePool.
func AfterDeleteLKENodePool(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLKENodePoolNode instruments (*Client).DeleteLKENodePoolNode.
func BeforeDeleteLKENodePoolNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteLKENodePoolNode finishes the span for (*Client).DeleteLKENodePoolNode.
func AfterDeleteLKENodePoolNode(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLock instruments (*Client).DeleteLock.
func BeforeDeleteLock(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLock finishes the span for (*Client).DeleteLock.
func AfterDeleteLock(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLogStream instruments (*Client).DeleteLogStream.
func BeforeDeleteLogStream(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLogStream finishes the span for (*Client).DeleteLogStream.
func AfterDeleteLogStream(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLogsDestination instruments (*Client).DeleteLogsDestination.
func BeforeDeleteLogsDestination(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLogsDestination finishes the span for (*Client).DeleteLogsDestination.
func AfterDeleteLogsDestination(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteLongviewClient instruments (*Client).DeleteLongviewClient.
func BeforeDeleteLongviewClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteLongviewClient finishes the span for (*Client).DeleteLongviewClient.
func AfterDeleteLongviewClient(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteMonitorAlertDefinition instruments (*Client).DeleteMonitorAlertDefinition.
func BeforeDeleteMonitorAlertDefinition(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteMonitorAlertDefinition finishes the span for (*Client).DeleteMonitorAlertDefinition.
func AfterDeleteMonitorAlertDefinition(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteMySQLDatabase instruments (*Client).DeleteMySQLDatabase.
func BeforeDeleteMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteMySQLDatabase finishes the span for (*Client).DeleteMySQLDatabase.
func AfterDeleteMySQLDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteNodeBalancer instruments (*Client).DeleteNodeBalancer.
func BeforeDeleteNodeBalancer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteNodeBalancer finishes the span for (*Client).DeleteNodeBalancer.
func AfterDeleteNodeBalancer(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteNodeBalancerConfig instruments (*Client).DeleteNodeBalancerConfig.
func BeforeDeleteNodeBalancerConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteNodeBalancerConfig finishes the span for (*Client).DeleteNodeBalancerConfig.
func AfterDeleteNodeBalancerConfig(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteNodeBalancerNode instruments (*Client).DeleteNodeBalancerNode.
func BeforeDeleteNodeBalancerNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterDeleteNodeBalancerNode finishes the span for (*Client).DeleteNodeBalancerNode.
func AfterDeleteNodeBalancerNode(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteOAuthClient instruments (*Client).DeleteOAuthClient.
func BeforeDeleteOAuthClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteOAuthClient finishes the span for (*Client).DeleteOAuthClient.
func AfterDeleteOAuthClient(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteObjectStorageBucket instruments (*Client).DeleteObjectStorageBucket.
func BeforeDeleteObjectStorageBucket(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteObjectStorageBucket finishes the span for (*Client).DeleteObjectStorageBucket.
func AfterDeleteObjectStorageBucket(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteObjectStorageBucketCert instruments (*Client).DeleteObjectStorageBucketCert.
func BeforeDeleteObjectStorageBucketCert(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteObjectStorageBucketCert finishes the span for (*Client).DeleteObjectStorageBucketCert.
func AfterDeleteObjectStorageBucketCert(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteObjectStorageKey instruments (*Client).DeleteObjectStorageKey.
func BeforeDeleteObjectStorageKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteObjectStorageKey finishes the span for (*Client).DeleteObjectStorageKey.
func AfterDeleteObjectStorageKey(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeletePaymentMethod instruments (*Client).DeletePaymentMethod.
func BeforeDeletePaymentMethod(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeletePaymentMethod finishes the span for (*Client).DeletePaymentMethod.
func AfterDeletePaymentMethod(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeletePhoneNumber instruments (*Client).DeletePhoneNumber.
func BeforeDeletePhoneNumber(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterDeletePhoneNumber finishes the span for (*Client).DeletePhoneNumber.
func AfterDeletePhoneNumber(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeletePlacementGroup instruments (*Client).DeletePlacementGroup.
func BeforeDeletePlacementGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeletePlacementGroup finishes the span for (*Client).DeletePlacementGroup.
func AfterDeletePlacementGroup(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeletePostgresDatabase instruments (*Client).DeletePostgresDatabase.
func BeforeDeletePostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeletePostgresDatabase finishes the span for (*Client).DeletePostgresDatabase.
func AfterDeletePostgresDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteProfileApp instruments (*Client).DeleteProfileApp.
func BeforeDeleteProfileApp(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteProfileApp finishes the span for (*Client).DeleteProfileApp.
func AfterDeleteProfileApp(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteProfileDevice instruments (*Client).DeleteProfileDevice.
func BeforeDeleteProfileDevice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteProfileDevice finishes the span for (*Client).DeleteProfileDevice.
func AfterDeleteProfileDevice(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteReservedIPAddress instruments (*Client).DeleteReservedIPAddress.
func BeforeDeleteReservedIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteReservedIPAddress finishes the span for (*Client).DeleteReservedIPAddress.
func AfterDeleteReservedIPAddress(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteSSHKey instruments (*Client).DeleteSSHKey.
func BeforeDeleteSSHKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteSSHKey finishes the span for (*Client).DeleteSSHKey.
func AfterDeleteSSHKey(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteStackscript instruments (*Client).DeleteStackscript.
func BeforeDeleteStackscript(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteStackscript finishes the span for (*Client).DeleteStackscript.
func AfterDeleteStackscript(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteTag instruments (*Client).DeleteTag.
func BeforeDeleteTag(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteTag finishes the span for (*Client).DeleteTag.
func AfterDeleteTag(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteToken instruments (*Client).DeleteToken.
func BeforeDeleteToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteToken finishes the span for (*Client).DeleteToken.
func AfterDeleteToken(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteUser instruments (*Client).DeleteUser.
func BeforeDeleteUser(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteUser finishes the span for (*Client).DeleteUser.
func AfterDeleteUser(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteVPC instruments (*Client).DeleteVPC.
func BeforeDeleteVPC(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteVPC finishes the span for (*Client).DeleteVPC.
func AfterDeleteVPC(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteVPCSubnet instruments (*Client).DeleteVPCSubnet.
func BeforeDeleteVPCSubnet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterDeleteVPCSubnet finishes the span for (*Client).DeleteVPCSubnet.
func AfterDeleteVPCSubnet(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDeleteVolume instruments (*Client).DeleteVolume.
func BeforeDeleteVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDeleteVolume finishes the span for (*Client).DeleteVolume.
func AfterDeleteVolume(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDetachVolume instruments (*Client).DetachVolume.
func BeforeDetachVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterDetachVolume finishes the span for (*Client).DetachVolume.
func AfterDetachVolume(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeDisableTwoFactor instruments (*Client).DisableTwoFactor.
func BeforeDisableTwoFactor(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterDisableTwoFactor finishes the span for (*Client).DisableTwoFactor.
func AfterDisableTwoFactor(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeEnableInstanceBackups instruments (*Client).EnableInstanceBackups.
func BeforeEnableInstanceBackups(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterEnableInstanceBackups finishes the span for (*Client).EnableInstanceBackups.
func AfterEnableInstanceBackups(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeGetAccount instruments (*Client).GetAccount.
func BeforeGetAccount(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetAccount finishes the span for (*Client).GetAccount.
func AfterGetAccount(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountAgreements instruments (*Client).GetAccountAgreements.
func BeforeGetAccountAgreements(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetAccountAgreements finishes the span for (*Client).GetAccountAgreements.
func AfterGetAccountAgreements(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountAvailability instruments (*Client).GetAccountAvailability.
func BeforeGetAccountAvailability(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetAccountAvailability finishes the span for (*Client).GetAccountAvailability.
func AfterGetAccountAvailability(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountBetaProgram instruments (*Client).GetAccountBetaProgram.
func BeforeGetAccountBetaProgram(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetAccountBetaProgram finishes the span for (*Client).GetAccountBetaProgram.
func AfterGetAccountBetaProgram(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountRolePermissions instruments (*Client).GetAccountRolePermissions.
func BeforeGetAccountRolePermissions(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetAccountRolePermissions finishes the span for (*Client).GetAccountRolePermissions.
func AfterGetAccountRolePermissions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountServiceTransfer instruments (*Client).GetAccountServiceTransfer.
func BeforeGetAccountServiceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetAccountServiceTransfer finishes the span for (*Client).GetAccountServiceTransfer.
func AfterGetAccountServiceTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountSettings instruments (*Client).GetAccountSettings.
func BeforeGetAccountSettings(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetAccountSettings finishes the span for (*Client).GetAccountSettings.
func AfterGetAccountSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetAccountTransfer instruments (*Client).GetAccountTransfer.
func BeforeGetAccountTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetAccountTransfer finishes the span for (*Client).GetAccountTransfer.
func AfterGetAccountTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetBetaProgram instruments (*Client).GetBetaProgram.
func BeforeGetBetaProgram(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetBetaProgram finishes the span for (*Client).GetBetaProgram.
func AfterGetBetaProgram(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetChildAccount instruments (*Client).GetChildAccount.
func BeforeGetChildAccount(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetChildAccount finishes the span for (*Client).GetChildAccount.
func AfterGetChildAccount(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetDatabaseEngine instruments (*Client).GetDatabaseEngine.
func BeforeGetDatabaseEngine(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetDatabaseEngine finishes the span for (*Client).GetDatabaseEngine.
func AfterGetDatabaseEngine(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetDatabaseType instruments (*Client).GetDatabaseType.
func BeforeGetDatabaseType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetDatabaseType finishes the span for (*Client).GetDatabaseType.
func AfterGetDatabaseType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetDomain instruments (*Client).GetDomain.
func BeforeGetDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetDomain finishes the span for (*Client).GetDomain.
func AfterGetDomain(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetDomainRecord instruments (*Client).GetDomainRecord.
func BeforeGetDomainRecord(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetDomainRecord finishes the span for (*Client).GetDomainRecord.
func AfterGetDomainRecord(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetDomainZoneFile instruments (*Client).GetDomainZoneFile.
func BeforeGetDomainZoneFile(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetDomainZoneFile finishes the span for (*Client).GetDomainZoneFile.
func AfterGetDomainZoneFile(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetEntityRoles instruments (*Client).GetEntityRoles.
func BeforeGetEntityRoles(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetEntityRoles finishes the span for (*Client).GetEntityRoles.
func AfterGetEntityRoles(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetEvent instruments (*Client).GetEvent.
func BeforeGetEvent(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetEvent finishes the span for (*Client).GetEvent.
func AfterGetEvent(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewall instruments (*Client).GetFirewall.
func BeforeGetFirewall(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetFirewall finishes the span for (*Client).GetFirewall.
func AfterGetFirewall(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallDevice instruments (*Client).GetFirewallDevice.
func BeforeGetFirewallDevice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetFirewallDevice finishes the span for (*Client).GetFirewallDevice.
func AfterGetFirewallDevice(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallRuleSet instruments (*Client).GetFirewallRuleSet.
func BeforeGetFirewallRuleSet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetFirewallRuleSet finishes the span for (*Client).GetFirewallRuleSet.
func AfterGetFirewallRuleSet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallRules instruments (*Client).GetFirewallRules.
func BeforeGetFirewallRules(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetFirewallRules finishes the span for (*Client).GetFirewallRules.
func AfterGetFirewallRules(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallRulesExpansion instruments (*Client).GetFirewallRulesExpansion.
func BeforeGetFirewallRulesExpansion(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetFirewallRulesExpansion finishes the span for (*Client).GetFirewallRulesExpansion.
func AfterGetFirewallRulesExpansion(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallSettings instruments (*Client).GetFirewallSettings.
func BeforeGetFirewallSettings(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetFirewallSettings finishes the span for (*Client).GetFirewallSettings.
func AfterGetFirewallSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetFirewallTemplate instruments (*Client).GetFirewallTemplate.
func BeforeGetFirewallTemplate(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetFirewallTemplate finishes the span for (*Client).GetFirewallTemplate.
func AfterGetFirewallTemplate(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetIPAddress instruments (*Client).GetIPAddress.
func BeforeGetIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetIPAddress finishes the span for (*Client).GetIPAddress.
func AfterGetIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetIPv6Pool instruments (*Client).GetIPv6Pool.
func BeforeGetIPv6Pool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetIPv6Pool finishes the span for (*Client).GetIPv6Pool.
func AfterGetIPv6Pool(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetIPv6Range instruments (*Client).GetIPv6Range.
func BeforeGetIPv6Range(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetIPv6Range finishes the span for (*Client).GetIPv6Range.
func AfterGetIPv6Range(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetImage instruments (*Client).GetImage.
func BeforeGetImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetImage finishes the span for (*Client).GetImage.
func AfterGetImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetImageShareGroup instruments (*Client).GetImageShareGroup.
func BeforeGetImageShareGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetImageShareGroup finishes the span for (*Client).GetImageShareGroup.
func AfterGetImageShareGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstance instruments (*Client).GetInstance.
func BeforeGetInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInstance finishes the span for (*Client).GetInstance.
func AfterGetInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceBackups instruments (*Client).GetInstanceBackups.
func BeforeGetInstanceBackups(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInstanceBackups finishes the span for (*Client).GetInstanceBackups.
func AfterGetInstanceBackups(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceConfig instruments (*Client).GetInstanceConfig.
func BeforeGetInstanceConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetInstanceConfig finishes the span for (*Client).GetInstanceConfig.
func AfterGetInstanceConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceConfigInterface instruments (*Client).GetInstanceConfigInterface.
func BeforeGetInstanceConfigInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetInstanceConfigInterface finishes the span for (*Client).GetInstanceConfigInterface.
func AfterGetInstanceConfigInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceDisk instruments (*Client).GetInstanceDisk.
func BeforeGetInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetInstanceDisk finishes the span for (*Client).GetInstanceDisk.
func AfterGetInstanceDisk(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceIPAddress instruments (*Client).GetInstanceIPAddress.
func BeforeGetInstanceIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetInstanceIPAddress finishes the span for (*Client).GetInstanceIPAddress.
func AfterGetInstanceIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceIPAddresses instruments (*Client).GetInstanceIPAddresses.
func BeforeGetInstanceIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInstanceIPAddresses finishes the span for (*Client).GetInstanceIPAddresses.
func AfterGetInstanceIPAddresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceSnapshot instruments (*Client).GetInstanceSnapshot.
func BeforeGetInstanceSnapshot(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetInstanceSnapshot finishes the span for (*Client).GetInstanceSnapshot.
func AfterGetInstanceSnapshot(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceStats instruments (*Client).GetInstanceStats.
func BeforeGetInstanceStats(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInstanceStats finishes the span for (*Client).GetInstanceStats.
func AfterGetInstanceStats(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceStatsByDate instruments (*Client).GetInstanceStatsByDate.
func BeforeGetInstanceStatsByDate(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetInstanceStatsByDate finishes the span for (*Client).GetInstanceStatsByDate.
func AfterGetInstanceStatsByDate(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceTransfer instruments (*Client).GetInstanceTransfer.
func BeforeGetInstanceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInstanceTransfer finishes the span for (*Client).GetInstanceTransfer.
func AfterGetInstanceTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInstanceTransferMonthly instruments (*Client).GetInstanceTransferMonthly.
func BeforeGetInstanceTransferMonthly(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetInstanceTransferMonthly finishes the span for (*Client).GetInstanceTransferMonthly.
func AfterGetInstanceTransferMonthly(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInterface instruments (*Client).GetInterface.
func BeforeGetInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetInterface finishes the span for (*Client).GetInterface.
func AfterGetInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInterfaceSettings instruments (*Client).GetInterfaceSettings.
func BeforeGetInterfaceSettings(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInterfaceSettings finishes the span for (*Client).GetInterfaceSettings.
func AfterGetInterfaceSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetInvoice instruments (*Client).GetInvoice.
func BeforeGetInvoice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetInvoice finishes the span for (*Client).GetInvoice.
func AfterGetInvoice(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetKernel instruments (*Client).GetKernel.
func BeforeGetKernel(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetKernel finishes the span for (*Client).GetKernel.
func AfterGetKernel(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKECluster instruments (*Client).GetLKECluster.
func BeforeGetLKECluster(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKECluster finishes the span for (*Client).GetLKECluster.
func AfterGetLKECluster(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKEClusterAPLConsoleURL instruments (*Client).GetLKEClusterAPLConsoleURL.
func BeforeGetLKEClusterAPLConsoleURL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKEClusterAPLConsoleURL finishes the span for (*Client).GetLKEClusterAPLConsoleURL.
func AfterGetLKEClusterAPLConsoleURL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKEClusterAPLHealthCheckURL instruments (*Client).GetLKEClusterAPLHealthCheckURL.
func BeforeGetLKEClusterAPLHealthCheckURL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKEClusterAPLHealthCheckURL finishes the span for (*Client).GetLKEClusterAPLHealthCheckURL.
func AfterGetLKEClusterAPLHealthCheckURL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKEClusterControlPlaneACL instruments (*Client).GetLKEClusterControlPlaneACL.
func BeforeGetLKEClusterControlPlaneACL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKEClusterControlPlaneACL finishes the span for (*Client).GetLKEClusterControlPlaneACL.
func AfterGetLKEClusterControlPlaneACL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKEClusterKubeconfig instruments (*Client).GetLKEClusterKubeconfig.
func BeforeGetLKEClusterKubeconfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKEClusterKubeconfig finishes the span for (*Client).GetLKEClusterKubeconfig.
func AfterGetLKEClusterKubeconfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKENodePool instruments (*Client).GetLKENodePool.
func BeforeGetLKENodePool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetLKENodePool finishes the span for (*Client).GetLKENodePool.
func AfterGetLKENodePool(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKENodePoolNode instruments (*Client).GetLKENodePoolNode.
func BeforeGetLKENodePoolNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetLKENodePoolNode finishes the span for (*Client).GetLKENodePoolNode.
func AfterGetLKENodePoolNode(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKETierVersion instruments (*Client).GetLKETierVersion.
func BeforeGetLKETierVersion(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetLKETierVersion finishes the span for (*Client).GetLKETierVersion.
func AfterGetLKETierVersion(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLKEVersion instruments (*Client).GetLKEVersion.
func BeforeGetLKEVersion(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLKEVersion finishes the span for (*Client).GetLKEVersion.
func AfterGetLKEVersion(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLock instruments (*Client).GetLock.
func BeforeGetLock(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLock finishes the span for (*Client).GetLock.
func AfterGetLock(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLogStream instruments (*Client).GetLogStream.
func BeforeGetLogStream(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLogStream finishes the span for (*Client).GetLogStream.
func AfterGetLogStream(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLogin instruments (*Client).GetLogin.
func BeforeGetLogin(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLogin finishes the span for (*Client).GetLogin.
func AfterGetLogin(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLogsDestination instruments (*Client).GetLogsDestination.
func BeforeGetLogsDestination(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLogsDestination finishes the span for (*Client).GetLogsDestination.
func AfterGetLogsDestination(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLongviewClient instruments (*Client).GetLongviewClient.
func BeforeGetLongviewClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLongviewClient finishes the span for (*Client).GetLongviewClient.
func AfterGetLongviewClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLongviewPlan instruments (*Client).GetLongviewPlan.
func BeforeGetLongviewPlan(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetLongviewPlan finishes the span for (*Client).GetLongviewPlan.
func AfterGetLongviewPlan(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetLongviewSubscription instruments (*Client).GetLongviewSubscription.
func BeforeGetLongviewSubscription(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetLongviewSubscription finishes the span for (*Client).GetLongviewSubscription.
func AfterGetLongviewSubscription(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMonitorAlertDefinition instruments (*Client).GetMonitorAlertDefinition.
func BeforeGetMonitorAlertDefinition(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetMonitorAlertDefinition finishes the span for (*Client).GetMonitorAlertDefinition.
func AfterGetMonitorAlertDefinition(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMonitorDashboard instruments (*Client).GetMonitorDashboard.
func BeforeGetMonitorDashboard(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetMonitorDashboard finishes the span for (*Client).GetMonitorDashboard.
func AfterGetMonitorDashboard(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMonitorServiceByType instruments (*Client).GetMonitorServiceByType.
func BeforeGetMonitorServiceByType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetMonitorServiceByType finishes the span for (*Client).GetMonitorServiceByType.
func AfterGetMonitorServiceByType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMySQLDatabase instruments (*Client).GetMySQLDatabase.
func BeforeGetMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetMySQLDatabase finishes the span for (*Client).GetMySQLDatabase.
func AfterGetMySQLDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMySQLDatabaseConfig instruments (*Client).GetMySQLDatabaseConfig.
func BeforeGetMySQLDatabaseConfig(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetMySQLDatabaseConfig finishes the span for (*Client).GetMySQLDatabaseConfig.
func AfterGetMySQLDatabaseConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMySQLDatabaseCredentials instruments (*Client).GetMySQLDatabaseCredentials.
func BeforeGetMySQLDatabaseCredentials(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetMySQLDatabaseCredentials finishes the span for (*Client).GetMySQLDatabaseCredentials.
func AfterGetMySQLDatabaseCredentials(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetMySQLDatabaseSSL instruments (*Client).GetMySQLDatabaseSSL.
func BeforeGetMySQLDatabaseSSL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetMySQLDatabaseSSL finishes the span for (*Client).GetMySQLDatabaseSSL.
func AfterGetMySQLDatabaseSSL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetNodeBalancer instruments (*Client).GetNodeBalancer.
func BeforeGetNodeBalancer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetNodeBalancer finishes the span for (*Client).GetNodeBalancer.
func AfterGetNodeBalancer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetNodeBalancerConfig instruments (*Client).GetNodeBalancerConfig.
func BeforeGetNodeBalancerConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetNodeBalancerConfig finishes the span for (*Client).GetNodeBalancerConfig.
func AfterGetNodeBalancerConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetNodeBalancerNode instruments (*Client).GetNodeBalancerNode.
func BeforeGetNodeBalancerNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetNodeBalancerNode finishes the span for (*Client).GetNodeBalancerNode.
func AfterGetNodeBalancerNode(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetNodeBalancerStats instruments (*Client).GetNodeBalancerStats.
func BeforeGetNodeBalancerStats(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetNodeBalancerStats finishes the span for (*Client).GetNodeBalancerStats.
func AfterGetNodeBalancerStats(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetNodeBalancerVPCConfig instruments (*Client).GetNodeBalancerVPCConfig.
func BeforeGetNodeBalancerVPCConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetNodeBalancerVPCConfig finishes the span for (*Client).GetNodeBalancerVPCConfig.
func AfterGetNodeBalancerVPCConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetOAuthClient instruments (*Client).GetOAuthClient.
func BeforeGetOAuthClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetOAuthClient finishes the span for (*Client).GetOAuthClient.
func AfterGetOAuthClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageBucket instruments (*Client).GetObjectStorageBucket.
func BeforeGetObjectStorageBucket(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetObjectStorageBucket finishes the span for (*Client).GetObjectStorageBucket.
func AfterGetObjectStorageBucket(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageBucketAccess instruments (*Client).GetObjectStorageBucketAccess.
func BeforeGetObjectStorageBucketAccess(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetObjectStorageBucketAccess finishes the span for (*Client).GetObjectStorageBucketAccess.
func AfterGetObjectStorageBucketAccess(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageBucketCert instruments (*Client).GetObjectStorageBucketCert.
func BeforeGetObjectStorageBucketCert(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetObjectStorageBucketCert finishes the span for (*Client).GetObjectStorageBucketCert.
func AfterGetObjectStorageBucketCert(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageGlobalQuota instruments (*Client).GetObjectStorageGlobalQuota.
func BeforeGetObjectStorageGlobalQuota(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetObjectStorageGlobalQuota finishes the span for (*Client).GetObjectStorageGlobalQuota.
func AfterGetObjectStorageGlobalQuota(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageGlobalQuotaUsage instruments (*Client).GetObjectStorageGlobalQuotaUsage.
func BeforeGetObjectStorageGlobalQuotaUsage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetObjectStorageGlobalQuotaUsage finishes the span for (*Client).GetObjectStorageGlobalQuotaUsage.
func AfterGetObjectStorageGlobalQuotaUsage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageKey instruments (*Client).GetObjectStorageKey.
func BeforeGetObjectStorageKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetObjectStorageKey finishes the span for (*Client).GetObjectStorageKey.
func AfterGetObjectStorageKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageObjectACLConfig instruments (*Client).GetObjectStorageObjectACLConfig.
func BeforeGetObjectStorageObjectACLConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterGetObjectStorageObjectACLConfig finishes the span for (*Client).GetObjectStorageObjectACLConfig.
func AfterGetObjectStorageObjectACLConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageQuota instruments (*Client).GetObjectStorageQuota.
func BeforeGetObjectStorageQuota(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetObjectStorageQuota finishes the span for (*Client).GetObjectStorageQuota.
func AfterGetObjectStorageQuota(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageQuotaUsage instruments (*Client).GetObjectStorageQuotaUsage.
func BeforeGetObjectStorageQuotaUsage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetObjectStorageQuotaUsage finishes the span for (*Client).GetObjectStorageQuotaUsage.
func AfterGetObjectStorageQuotaUsage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetObjectStorageTransfer instruments (*Client).GetObjectStorageTransfer.
func BeforeGetObjectStorageTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetObjectStorageTransfer finishes the span for (*Client).GetObjectStorageTransfer.
func AfterGetObjectStorageTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPayment instruments (*Client).GetPayment.
func BeforeGetPayment(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPayment finishes the span for (*Client).GetPayment.
func AfterGetPayment(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPaymentMethod instruments (*Client).GetPaymentMethod.
func BeforeGetPaymentMethod(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPaymentMethod finishes the span for (*Client).GetPaymentMethod.
func AfterGetPaymentMethod(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPlacementGroup instruments (*Client).GetPlacementGroup.
func BeforeGetPlacementGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPlacementGroup finishes the span for (*Client).GetPlacementGroup.
func AfterGetPlacementGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPostgresDatabase instruments (*Client).GetPostgresDatabase.
func BeforeGetPostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPostgresDatabase finishes the span for (*Client).GetPostgresDatabase.
func AfterGetPostgresDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPostgresDatabaseConfig instruments (*Client).GetPostgresDatabaseConfig.
func BeforeGetPostgresDatabaseConfig(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetPostgresDatabaseConfig finishes the span for (*Client).GetPostgresDatabaseConfig.
func AfterGetPostgresDatabaseConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPostgresDatabaseCredentials instruments (*Client).GetPostgresDatabaseCredentials.
func BeforeGetPostgresDatabaseCredentials(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPostgresDatabaseCredentials finishes the span for (*Client).GetPostgresDatabaseCredentials.
func AfterGetPostgresDatabaseCredentials(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPostgresDatabaseSSL instruments (*Client).GetPostgresDatabaseSSL.
func BeforeGetPostgresDatabaseSSL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPostgresDatabaseSSL finishes the span for (*Client).GetPostgresDatabaseSSL.
func AfterGetPostgresDatabaseSSL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetPrefixList instruments (*Client).GetPrefixList.
func BeforeGetPrefixList(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetPrefixList finishes the span for (*Client).GetPrefixList.
func AfterGetPrefixList(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetProfile instruments (*Client).GetProfile.
func BeforeGetProfile(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetProfile finishes the span for (*Client).GetProfile.
func AfterGetProfile(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetProfileApp instruments (*Client).GetProfileApp.
func BeforeGetProfileApp(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetProfileApp finishes the span for (*Client).GetProfileApp.
func AfterGetProfileApp(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetProfileDevice instruments (*Client).GetProfileDevice.
func BeforeGetProfileDevice(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetProfileDevice finishes the span for (*Client).GetProfileDevice.
func AfterGetProfileDevice(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetProfileLogin instruments (*Client).GetProfileLogin.
func BeforeGetProfileLogin(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetProfileLogin finishes the span for (*Client).GetProfileLogin.
func AfterGetProfileLogin(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetProfilePreferences instruments (*Client).GetProfilePreferences.
func BeforeGetProfilePreferences(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetProfilePreferences finishes the span for (*Client).GetProfilePreferences.
func AfterGetProfilePreferences(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetRegion instruments (*Client).GetRegion.
func BeforeGetRegion(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetRegion finishes the span for (*Client).GetRegion.
func AfterGetRegion(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetRegionAvailability instruments (*Client).GetRegionAvailability.
func BeforeGetRegionAvailability(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetRegionAvailability finishes the span for (*Client).GetRegionAvailability.
func AfterGetRegionAvailability(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetRegionVPCAvailability instruments (*Client).GetRegionVPCAvailability.
func BeforeGetRegionVPCAvailability(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetRegionVPCAvailability finishes the span for (*Client).GetRegionVPCAvailability.
func AfterGetRegionVPCAvailability(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetReservedIPAddress instruments (*Client).GetReservedIPAddress.
func BeforeGetReservedIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetReservedIPAddress finishes the span for (*Client).GetReservedIPAddress.
func AfterGetReservedIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetSSHKey instruments (*Client).GetSSHKey.
func BeforeGetSSHKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetSSHKey finishes the span for (*Client).GetSSHKey.
func AfterGetSSHKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetStackscript instruments (*Client).GetStackscript.
func BeforeGetStackscript(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetStackscript finishes the span for (*Client).GetStackscript.
func AfterGetStackscript(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetTicket instruments (*Client).GetTicket.
func BeforeGetTicket(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetTicket finishes the span for (*Client).GetTicket.
func AfterGetTicket(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetToken instruments (*Client).GetToken.
func BeforeGetToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetToken finishes the span for (*Client).GetToken.
func AfterGetToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetType instruments (*Client).GetType.
func BeforeGetType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetType finishes the span for (*Client).GetType.
func AfterGetType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetUser instruments (*Client).GetUser.
func BeforeGetUser(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetUser finishes the span for (*Client).GetUser.
func AfterGetUser(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetUserAccountPermissions instruments (*Client).GetUserAccountPermissions.
func BeforeGetUserAccountPermissions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetUserAccountPermissions finishes the span for (*Client).GetUserAccountPermissions.
func AfterGetUserAccountPermissions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetUserGrants instruments (*Client).GetUserGrants.
func BeforeGetUserGrants(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetUserGrants finishes the span for (*Client).GetUserGrants.
func AfterGetUserGrants(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetUserRolePermissions instruments (*Client).GetUserRolePermissions.
func BeforeGetUserRolePermissions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetUserRolePermissions finishes the span for (*Client).GetUserRolePermissions.
func AfterGetUserRolePermissions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetVLANIPAMAddress instruments (*Client).GetVLANIPAMAddress.
func BeforeGetVLANIPAMAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetVLANIPAMAddress finishes the span for (*Client).GetVLANIPAMAddress.
func AfterGetVLANIPAMAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetVPC instruments (*Client).GetVPC.
func BeforeGetVPC(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetVPC finishes the span for (*Client).GetVPC.
func AfterGetVPC(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetVPCDefaultRanges instruments (*Client).GetVPCDefaultRanges.
func BeforeGetVPCDefaultRanges(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGetVPCDefaultRanges finishes the span for (*Client).GetVPCDefaultRanges.
func AfterGetVPCDefaultRanges(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetVPCSubnet instruments (*Client).GetVPCSubnet.
func BeforeGetVPCSubnet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterGetVPCSubnet finishes the span for (*Client).GetVPCSubnet.
func AfterGetVPCSubnet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGetVolume instruments (*Client).GetVolume.
func BeforeGetVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterGetVolume finishes the span for (*Client).GetVolume.
func AfterGetVolume(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeGrantsList instruments (*Client).GrantsList.
func BeforeGrantsList(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterGrantsList finishes the span for (*Client).GrantsList.
func AfterGrantsList(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupAddImages instruments (*Client).ImageShareGroupAddImages.
func BeforeImageShareGroupAddImages(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupAddImages finishes the span for (*Client).ImageShareGroupAddImages.
func AfterImageShareGroupAddImages(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupAddMember instruments (*Client).ImageShareGroupAddMember.
func BeforeImageShareGroupAddMember(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupAddMember finishes the span for (*Client).ImageShareGroupAddMember.
func AfterImageShareGroupAddMember(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupCreateToken instruments (*Client).ImageShareGroupCreateToken.
func BeforeImageShareGroupCreateToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImageShareGroupCreateToken finishes the span for (*Client).ImageShareGroupCreateToken.
func AfterImageShareGroupCreateToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupGetByToken instruments (*Client).ImageShareGroupGetByToken.
func BeforeImageShareGroupGetByToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImageShareGroupGetByToken finishes the span for (*Client).ImageShareGroupGetByToken.
func AfterImageShareGroupGetByToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupGetImageShareEntriesByToken instruments (*Client).ImageShareGroupGetImageShareEntriesByToken.
func BeforeImageShareGroupGetImageShareEntriesByToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupGetImageShareEntriesByToken finishes the span for (*Client).ImageShareGroupGetImageShareEntriesByToken.
func AfterImageShareGroupGetImageShareEntriesByToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupGetMember instruments (*Client).ImageShareGroupGetMember.
func BeforeImageShareGroupGetMember(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupGetMember finishes the span for (*Client).ImageShareGroupGetMember.
func AfterImageShareGroupGetMember(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupGetToken instruments (*Client).ImageShareGroupGetToken.
func BeforeImageShareGroupGetToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImageShareGroupGetToken finishes the span for (*Client).ImageShareGroupGetToken.
func AfterImageShareGroupGetToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupListImageShareEntries instruments (*Client).ImageShareGroupListImageShareEntries.
func BeforeImageShareGroupListImageShareEntries(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupListImageShareEntries finishes the span for (*Client).ImageShareGroupListImageShareEntries.
func AfterImageShareGroupListImageShareEntries(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupListMembers instruments (*Client).ImageShareGroupListMembers.
func BeforeImageShareGroupListMembers(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupListMembers finishes the span for (*Client).ImageShareGroupListMembers.
func AfterImageShareGroupListMembers(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupListTokens instruments (*Client).ImageShareGroupListTokens.
func BeforeImageShareGroupListTokens(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImageShareGroupListTokens finishes the span for (*Client).ImageShareGroupListTokens.
func AfterImageShareGroupListTokens(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupRemoveImage instruments (*Client).ImageShareGroupRemoveImage.
func BeforeImageShareGroupRemoveImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupRemoveImage finishes the span for (*Client).ImageShareGroupRemoveImage.
func AfterImageShareGroupRemoveImage(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeImageShareGroupRemoveMember instruments (*Client).ImageShareGroupRemoveMember.
func BeforeImageShareGroupRemoveMember(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupRemoveMember finishes the span for (*Client).ImageShareGroupRemoveMember.
func AfterImageShareGroupRemoveMember(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeImageShareGroupRemoveToken instruments (*Client).ImageShareGroupRemoveToken.
func BeforeImageShareGroupRemoveToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImageShareGroupRemoveToken finishes the span for (*Client).ImageShareGroupRemoveToken.
func AfterImageShareGroupRemoveToken(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeImageShareGroupUpdateImageShareEntry instruments (*Client).ImageShareGroupUpdateImageShareEntry.
func BeforeImageShareGroupUpdateImageShareEntry(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterImageShareGroupUpdateImageShareEntry finishes the span for (*Client).ImageShareGroupUpdateImageShareEntry.
func AfterImageShareGroupUpdateImageShareEntry(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupUpdateMember instruments (*Client).ImageShareGroupUpdateMember.
func BeforeImageShareGroupUpdateMember(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterImageShareGroupUpdateMember finishes the span for (*Client).ImageShareGroupUpdateMember.
func AfterImageShareGroupUpdateMember(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImageShareGroupUpdateToken instruments (*Client).ImageShareGroupUpdateToken.
func BeforeImageShareGroupUpdateToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterImageShareGroupUpdateToken finishes the span for (*Client).ImageShareGroupUpdateToken.
func AfterImageShareGroupUpdateToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeImportDomain instruments (*Client).ImportDomain.
func BeforeImportDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterImportDomain finishes the span for (*Client).ImportDomain.
func AfterImportDomain(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeInstancesAssignIPs instruments (*Client).InstancesAssignIPs.
func BeforeInstancesAssignIPs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterInstancesAssignIPs finishes the span for (*Client).InstancesAssignIPs.
func AfterInstancesAssignIPs(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeJoinBetaProgram instruments (*Client).JoinBetaProgram.
func BeforeJoinBetaProgram(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterJoinBetaProgram finishes the span for (*Client).JoinBetaProgram.
func AfterJoinBetaProgram(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAccountAvailabilities instruments (*Client).ListAccountAvailabilities.
func BeforeListAccountAvailabilities(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAccountAvailabilities finishes the span for (*Client).ListAccountAvailabilities.
func AfterListAccountAvailabilities(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAccountBetaPrograms instruments (*Client).ListAccountBetaPrograms.
func BeforeListAccountBetaPrograms(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAccountBetaPrograms finishes the span for (*Client).ListAccountBetaPrograms.
func AfterListAccountBetaPrograms(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAccountServiceTransfer instruments (*Client).ListAccountServiceTransfer.
func BeforeListAccountServiceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAccountServiceTransfer finishes the span for (*Client).ListAccountServiceTransfer.
func AfterListAccountServiceTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAlertChannels instruments (*Client).ListAlertChannels.
func BeforeListAlertChannels(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAlertChannels finishes the span for (*Client).ListAlertChannels.
func AfterListAlertChannels(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAllMonitorAlertDefinitions instruments (*Client).ListAllMonitorAlertDefinitions.
func BeforeListAllMonitorAlertDefinitions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAllMonitorAlertDefinitions finishes the span for (*Client).ListAllMonitorAlertDefinitions.
func AfterListAllMonitorAlertDefinitions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAllVPCIPAddresses instruments (*Client).ListAllVPCIPAddresses.
func BeforeListAllVPCIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAllVPCIPAddresses finishes the span for (*Client).ListAllVPCIPAddresses.
func AfterListAllVPCIPAddresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListAllVPCIPv6Addresses instruments (*Client).ListAllVPCIPv6Addresses.
func BeforeListAllVPCIPv6Addresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListAllVPCIPv6Addresses finishes the span for (*Client).ListAllVPCIPv6Addresses.
func AfterListAllVPCIPv6Addresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListBetaPrograms instruments (*Client).ListBetaPrograms.
func BeforeListBetaPrograms(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListBetaPrograms finishes the span for (*Client).ListBetaPrograms.
func AfterListBetaPrograms(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListChildAccounts instruments (*Client).ListChildAccounts.
func BeforeListChildAccounts(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListChildAccounts finishes the span for (*Client).ListChildAccounts.
func AfterListChildAccounts(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListDatabaseEngines instruments (*Client).ListDatabaseEngines.
func BeforeListDatabaseEngines(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListDatabaseEngines finishes the span for (*Client).ListDatabaseEngines.
func AfterListDatabaseEngines(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListDatabaseTypes instruments (*Client).ListDatabaseTypes.
func BeforeListDatabaseTypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListDatabaseTypes finishes the span for (*Client).ListDatabaseTypes.
func AfterListDatabaseTypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListDatabases instruments (*Client).ListDatabases.
func BeforeListDatabases(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListDatabases finishes the span for (*Client).ListDatabases.
func AfterListDatabases(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListDomainRecords instruments (*Client).ListDomainRecords.
func BeforeListDomainRecords(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListDomainRecords finishes the span for (*Client).ListDomainRecords.
func AfterListDomainRecords(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListDomains instruments (*Client).ListDomains.
func BeforeListDomains(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListDomains finishes the span for (*Client).ListDomains.
func AfterListDomains(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListEntities instruments (*Client).ListEntities.
func BeforeListEntities(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListEntities finishes the span for (*Client).ListEntities.
func AfterListEntities(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListEvents instruments (*Client).ListEvents.
func BeforeListEvents(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListEvents finishes the span for (*Client).ListEvents.
func AfterListEvents(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListFirewallDevices instruments (*Client).ListFirewallDevices.
func BeforeListFirewallDevices(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListFirewallDevices finishes the span for (*Client).ListFirewallDevices.
func AfterListFirewallDevices(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListFirewallRuleSets instruments (*Client).ListFirewallRuleSets.
func BeforeListFirewallRuleSets(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListFirewallRuleSets finishes the span for (*Client).ListFirewallRuleSets.
func AfterListFirewallRuleSets(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListFirewallTemplates instruments (*Client).ListFirewallTemplates.
func BeforeListFirewallTemplates(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListFirewallTemplates finishes the span for (*Client).ListFirewallTemplates.
func AfterListFirewallTemplates(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListFirewalls instruments (*Client).ListFirewalls.
func BeforeListFirewalls(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListFirewalls finishes the span for (*Client).ListFirewalls.
func AfterListFirewalls(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListIPAddresses instruments (*Client).ListIPAddresses.
func BeforeListIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListIPAddresses finishes the span for (*Client).ListIPAddresses.
func AfterListIPAddresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListIPv6Pools instruments (*Client).ListIPv6Pools.
func BeforeListIPv6Pools(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListIPv6Pools finishes the span for (*Client).ListIPv6Pools.
func AfterListIPv6Pools(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListIPv6Ranges instruments (*Client).ListIPv6Ranges.
func BeforeListIPv6Ranges(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListIPv6Ranges finishes the span for (*Client).ListIPv6Ranges.
func AfterListIPv6Ranges(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListImageShareGroups instruments (*Client).ListImageShareGroups.
func BeforeListImageShareGroups(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListImageShareGroups finishes the span for (*Client).ListImageShareGroups.
func AfterListImageShareGroups(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListImageShareGroupsContainingPrivateImage instruments (*Client).ListImageShareGroupsContainingPrivateImage.
func BeforeListImageShareGroupsContainingPrivateImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListImageShareGroupsContainingPrivateImage finishes the span for (*Client).ListImageShareGroupsContainingPrivateImage.
func AfterListImageShareGroupsContainingPrivateImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListImages instruments (*Client).ListImages.
func BeforeListImages(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListImages finishes the span for (*Client).ListImages.
func AfterListImages(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceConfigInterfaces instruments (*Client).ListInstanceConfigInterfaces.
func BeforeListInstanceConfigInterfaces(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceConfigInterfaces finishes the span for (*Client).ListInstanceConfigInterfaces.
func AfterListInstanceConfigInterfaces(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceConfigs instruments (*Client).ListInstanceConfigs.
func BeforeListInstanceConfigs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceConfigs finishes the span for (*Client).ListInstanceConfigs.
func AfterListInstanceConfigs(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceDisks instruments (*Client).ListInstanceDisks.
func BeforeListInstanceDisks(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceDisks finishes the span for (*Client).ListInstanceDisks.
func AfterListInstanceDisks(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceFirewalls instruments (*Client).ListInstanceFirewalls.
func BeforeListInstanceFirewalls(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceFirewalls finishes the span for (*Client).ListInstanceFirewalls.
func AfterListInstanceFirewalls(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceNodeBalancers instruments (*Client).ListInstanceNodeBalancers.
func BeforeListInstanceNodeBalancers(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceNodeBalancers finishes the span for (*Client).ListInstanceNodeBalancers.
func AfterListInstanceNodeBalancers(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstanceVolumes instruments (*Client).ListInstanceVolumes.
func BeforeListInstanceVolumes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInstanceVolumes finishes the span for (*Client).ListInstanceVolumes.
func AfterListInstanceVolumes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInstances instruments (*Client).ListInstances.
func BeforeListInstances(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListInstances finishes the span for (*Client).ListInstances.
func AfterListInstances(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInterfaceFirewalls instruments (*Client).ListInterfaceFirewalls.
func BeforeListInterfaceFirewalls(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterListInterfaceFirewalls finishes the span for (*Client).ListInterfaceFirewalls.
func AfterListInterfaceFirewalls(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInterfaces instruments (*Client).ListInterfaces.
func BeforeListInterfaces(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInterfaces finishes the span for (*Client).ListInterfaces.
func AfterListInterfaces(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInvoiceItems instruments (*Client).ListInvoiceItems.
func BeforeListInvoiceItems(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListInvoiceItems finishes the span for (*Client).ListInvoiceItems.
func AfterListInvoiceItems(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListInvoices instruments (*Client).ListInvoices.
func BeforeListInvoices(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListInvoices finishes the span for (*Client).ListInvoices.
func AfterListInvoices(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListKernels instruments (*Client).ListKernels.
func BeforeListKernels(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListKernels finishes the span for (*Client).ListKernels.
func AfterListKernels(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKEClusterAPIEndpoints instruments (*Client).ListLKEClusterAPIEndpoints.
func BeforeListLKEClusterAPIEndpoints(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListLKEClusterAPIEndpoints finishes the span for (*Client).ListLKEClusterAPIEndpoints.
func AfterListLKEClusterAPIEndpoints(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKEClusters instruments (*Client).ListLKEClusters.
func BeforeListLKEClusters(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLKEClusters finishes the span for (*Client).ListLKEClusters.
func AfterListLKEClusters(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKENodePools instruments (*Client).ListLKENodePools.
func BeforeListLKENodePools(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListLKENodePools finishes the span for (*Client).ListLKENodePools.
func AfterListLKENodePools(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKETierVersions instruments (*Client).ListLKETierVersions.
func BeforeListLKETierVersions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListLKETierVersions finishes the span for (*Client).ListLKETierVersions.
func AfterListLKETierVersions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKETypes instruments (*Client).ListLKETypes.
func BeforeListLKETypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLKETypes finishes the span for (*Client).ListLKETypes.
func AfterListLKETypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLKEVersions instruments (*Client).ListLKEVersions.
func BeforeListLKEVersions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLKEVersions finishes the span for (*Client).ListLKEVersions.
func AfterListLKEVersions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLocks instruments (*Client).ListLocks.
func BeforeListLocks(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLocks finishes the span for (*Client).ListLocks.
func AfterListLocks(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLogStreamHistory instruments (*Client).ListLogStreamHistory.
func BeforeListLogStreamHistory(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListLogStreamHistory finishes the span for (*Client).ListLogStreamHistory.
func AfterListLogStreamHistory(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLogStreams instruments (*Client).ListLogStreams.
func BeforeListLogStreams(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLogStreams finishes the span for (*Client).ListLogStreams.
func AfterListLogStreams(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLogins instruments (*Client).ListLogins.
func BeforeListLogins(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLogins finishes the span for (*Client).ListLogins.
func AfterListLogins(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLogsDestinationHistory instruments (*Client).ListLogsDestinationHistory.
func BeforeListLogsDestinationHistory(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListLogsDestinationHistory finishes the span for (*Client).ListLogsDestinationHistory.
func AfterListLogsDestinationHistory(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLogsDestinations instruments (*Client).ListLogsDestinations.
func BeforeListLogsDestinations(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLogsDestinations finishes the span for (*Client).ListLogsDestinations.
func AfterListLogsDestinations(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLongviewClients instruments (*Client).ListLongviewClients.
func BeforeListLongviewClients(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLongviewClients finishes the span for (*Client).ListLongviewClients.
func AfterListLongviewClients(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListLongviewSubscriptions instruments (*Client).ListLongviewSubscriptions.
func BeforeListLongviewSubscriptions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListLongviewSubscriptions finishes the span for (*Client).ListLongviewSubscriptions.
func AfterListLongviewSubscriptions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMaintenancePolicies instruments (*Client).ListMaintenancePolicies.
func BeforeListMaintenancePolicies(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListMaintenancePolicies finishes the span for (*Client).ListMaintenancePolicies.
func AfterListMaintenancePolicies(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMaintenances instruments (*Client).ListMaintenances.
func BeforeListMaintenances(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListMaintenances finishes the span for (*Client).ListMaintenances.
func AfterListMaintenances(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorAlertDefinitionEntities instruments (*Client).ListMonitorAlertDefinitionEntities.
func BeforeListMonitorAlertDefinitionEntities(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterListMonitorAlertDefinitionEntities finishes the span for (*Client).ListMonitorAlertDefinitionEntities.
func AfterListMonitorAlertDefinitionEntities(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorAlertDefinitions instruments (*Client).ListMonitorAlertDefinitions.
func BeforeListMonitorAlertDefinitions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListMonitorAlertDefinitions finishes the span for (*Client).ListMonitorAlertDefinitions.
func AfterListMonitorAlertDefinitions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorDashboards instruments (*Client).ListMonitorDashboards.
func BeforeListMonitorDashboards(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListMonitorDashboards finishes the span for (*Client).ListMonitorDashboards.
func AfterListMonitorDashboards(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorDashboardsByServiceType instruments (*Client).ListMonitorDashboardsByServiceType.
func BeforeListMonitorDashboardsByServiceType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListMonitorDashboardsByServiceType finishes the span for (*Client).ListMonitorDashboardsByServiceType.
func AfterListMonitorDashboardsByServiceType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorMetricsDefinitionByServiceType instruments (*Client).ListMonitorMetricsDefinitionByServiceType.
func BeforeListMonitorMetricsDefinitionByServiceType(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListMonitorMetricsDefinitionByServiceType finishes the span for (*Client).ListMonitorMetricsDefinitionByServiceType.
func AfterListMonitorMetricsDefinitionByServiceType(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMonitorServices instruments (*Client).ListMonitorServices.
func BeforeListMonitorServices(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListMonitorServices finishes the span for (*Client).ListMonitorServices.
func AfterListMonitorServices(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListMySQLDatabases instruments (*Client).ListMySQLDatabases.
func BeforeListMySQLDatabases(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListMySQLDatabases finishes the span for (*Client).ListMySQLDatabases.
func AfterListMySQLDatabases(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNetworkTransferPrices instruments (*Client).ListNetworkTransferPrices.
func BeforeListNetworkTransferPrices(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListNetworkTransferPrices finishes the span for (*Client).ListNetworkTransferPrices.
func AfterListNetworkTransferPrices(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancerConfigs instruments (*Client).ListNodeBalancerConfigs.
func BeforeListNodeBalancerConfigs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListNodeBalancerConfigs finishes the span for (*Client).ListNodeBalancerConfigs.
func AfterListNodeBalancerConfigs(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancerFirewalls instruments (*Client).ListNodeBalancerFirewalls.
func BeforeListNodeBalancerFirewalls(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListNodeBalancerFirewalls finishes the span for (*Client).ListNodeBalancerFirewalls.
func AfterListNodeBalancerFirewalls(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancerNodes instruments (*Client).ListNodeBalancerNodes.
func BeforeListNodeBalancerNodes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterListNodeBalancerNodes finishes the span for (*Client).ListNodeBalancerNodes.
func AfterListNodeBalancerNodes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancerTypes instruments (*Client).ListNodeBalancerTypes.
func BeforeListNodeBalancerTypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListNodeBalancerTypes finishes the span for (*Client).ListNodeBalancerTypes.
func AfterListNodeBalancerTypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancerVPCConfigs instruments (*Client).ListNodeBalancerVPCConfigs.
func BeforeListNodeBalancerVPCConfigs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListNodeBalancerVPCConfigs finishes the span for (*Client).ListNodeBalancerVPCConfigs.
func AfterListNodeBalancerVPCConfigs(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNodeBalancers instruments (*Client).ListNodeBalancers.
func BeforeListNodeBalancers(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListNodeBalancers finishes the span for (*Client).ListNodeBalancers.
func AfterListNodeBalancers(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListNotifications instruments (*Client).ListNotifications.
func BeforeListNotifications(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListNotifications finishes the span for (*Client).ListNotifications.
func AfterListNotifications(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListOAuthClients instruments (*Client).ListOAuthClients.
func BeforeListOAuthClients(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListOAuthClients finishes the span for (*Client).ListOAuthClients.
func AfterListOAuthClients(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageBucketContents instruments (*Client).ListObjectStorageBucketContents.
func BeforeListObjectStorageBucketContents(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterListObjectStorageBucketContents finishes the span for (*Client).ListObjectStorageBucketContents.
func AfterListObjectStorageBucketContents(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageBuckets instruments (*Client).ListObjectStorageBuckets.
func BeforeListObjectStorageBuckets(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListObjectStorageBuckets finishes the span for (*Client).ListObjectStorageBuckets.
func AfterListObjectStorageBuckets(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageBucketsInRegion instruments (*Client).ListObjectStorageBucketsInRegion.
func BeforeListObjectStorageBucketsInRegion(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListObjectStorageBucketsInRegion finishes the span for (*Client).ListObjectStorageBucketsInRegion.
func AfterListObjectStorageBucketsInRegion(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageEndpoints instruments (*Client).ListObjectStorageEndpoints.
func BeforeListObjectStorageEndpoints(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListObjectStorageEndpoints finishes the span for (*Client).ListObjectStorageEndpoints.
func AfterListObjectStorageEndpoints(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageGlobalQuotas instruments (*Client).ListObjectStorageGlobalQuotas.
func BeforeListObjectStorageGlobalQuotas(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListObjectStorageGlobalQuotas finishes the span for (*Client).ListObjectStorageGlobalQuotas.
func AfterListObjectStorageGlobalQuotas(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageKeys instruments (*Client).ListObjectStorageKeys.
func BeforeListObjectStorageKeys(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListObjectStorageKeys finishes the span for (*Client).ListObjectStorageKeys.
func AfterListObjectStorageKeys(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListObjectStorageQuotas instruments (*Client).ListObjectStorageQuotas.
func BeforeListObjectStorageQuotas(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListObjectStorageQuotas finishes the span for (*Client).ListObjectStorageQuotas.
func AfterListObjectStorageQuotas(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListPaymentMethods instruments (*Client).ListPaymentMethods.
func BeforeListPaymentMethods(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListPaymentMethods finishes the span for (*Client).ListPaymentMethods.
func AfterListPaymentMethods(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListPayments instruments (*Client).ListPayments.
func BeforeListPayments(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListPayments finishes the span for (*Client).ListPayments.
func AfterListPayments(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListPlacementGroups instruments (*Client).ListPlacementGroups.
func BeforeListPlacementGroups(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListPlacementGroups finishes the span for (*Client).ListPlacementGroups.
func AfterListPlacementGroups(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListPostgresDatabases instruments (*Client).ListPostgresDatabases.
func BeforeListPostgresDatabases(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListPostgresDatabases finishes the span for (*Client).ListPostgresDatabases.
func AfterListPostgresDatabases(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListPrefixLists instruments (*Client).ListPrefixLists.
func BeforeListPrefixLists(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListPrefixLists finishes the span for (*Client).ListPrefixLists.
func AfterListPrefixLists(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListProfileApps instruments (*Client).ListProfileApps.
func BeforeListProfileApps(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListProfileApps finishes the span for (*Client).ListProfileApps.
func AfterListProfileApps(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListProfileDevices instruments (*Client).ListProfileDevices.
func BeforeListProfileDevices(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListProfileDevices finishes the span for (*Client).ListProfileDevices.
func AfterListProfileDevices(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListProfileLogins instruments (*Client).ListProfileLogins.
func BeforeListProfileLogins(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListProfileLogins finishes the span for (*Client).ListProfileLogins.
func AfterListProfileLogins(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListRegions instruments (*Client).ListRegions.
func BeforeListRegions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListRegions finishes the span for (*Client).ListRegions.
func AfterListRegions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListRegionsAvailability instruments (*Client).ListRegionsAvailability.
func BeforeListRegionsAvailability(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListRegionsAvailability finishes the span for (*Client).ListRegionsAvailability.
func AfterListRegionsAvailability(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListRegionsVPCAvailability instruments (*Client).ListRegionsVPCAvailability.
func BeforeListRegionsVPCAvailability(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListRegionsVPCAvailability finishes the span for (*Client).ListRegionsVPCAvailability.
func AfterListRegionsVPCAvailability(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListReservedIPAddresses instruments (*Client).ListReservedIPAddresses.
func BeforeListReservedIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListReservedIPAddresses finishes the span for (*Client).ListReservedIPAddresses.
func AfterListReservedIPAddresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListReservedIPTypes instruments (*Client).ListReservedIPTypes.
func BeforeListReservedIPTypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListReservedIPTypes finishes the span for (*Client).ListReservedIPTypes.
func AfterListReservedIPTypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListSSHKeys instruments (*Client).ListSSHKeys.
func BeforeListSSHKeys(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListSSHKeys finishes the span for (*Client).ListSSHKeys.
func AfterListSSHKeys(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListStackscripts instruments (*Client).ListStackscripts.
func BeforeListStackscripts(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListStackscripts finishes the span for (*Client).ListStackscripts.
func AfterListStackscripts(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListTaggedObjects instruments (*Client).ListTaggedObjects.
func BeforeListTaggedObjects(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListTaggedObjects finishes the span for (*Client).ListTaggedObjects.
func AfterListTaggedObjects(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListTags instruments (*Client).ListTags.
func BeforeListTags(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListTags finishes the span for (*Client).ListTags.
func AfterListTags(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListTickets instruments (*Client).ListTickets.
func BeforeListTickets(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListTickets finishes the span for (*Client).ListTickets.
func AfterListTickets(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListTokens instruments (*Client).ListTokens.
func BeforeListTokens(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListTokens finishes the span for (*Client).ListTokens.
func AfterListTokens(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListTypes instruments (*Client).ListTypes.
func BeforeListTypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListTypes finishes the span for (*Client).ListTypes.
func AfterListTypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListUsers instruments (*Client).ListUsers.
func BeforeListUsers(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListUsers finishes the span for (*Client).ListUsers.
func AfterListUsers(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVLANs instruments (*Client).ListVLANs.
func BeforeListVLANs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListVLANs finishes the span for (*Client).ListVLANs.
func AfterListVLANs(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVPCIPAddresses instruments (*Client).ListVPCIPAddresses.
func BeforeListVPCIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListVPCIPAddresses finishes the span for (*Client).ListVPCIPAddresses.
func AfterListVPCIPAddresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVPCIPv6Addresses instruments (*Client).ListVPCIPv6Addresses.
func BeforeListVPCIPv6Addresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListVPCIPv6Addresses finishes the span for (*Client).ListVPCIPv6Addresses.
func AfterListVPCIPv6Addresses(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVPCSubnets instruments (*Client).ListVPCSubnets.
func BeforeListVPCSubnets(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterListVPCSubnets finishes the span for (*Client).ListVPCSubnets.
func AfterListVPCSubnets(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVPCs instruments (*Client).ListVPCs.
func BeforeListVPCs(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListVPCs finishes the span for (*Client).ListVPCs.
func AfterListVPCs(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVolumeTypes instruments (*Client).ListVolumeTypes.
func BeforeListVolumeTypes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListVolumeTypes finishes the span for (*Client).ListVolumeTypes.
func AfterListVolumeTypes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeListVolumes instruments (*Client).ListVolumes.
func BeforeListVolumes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterListVolumes finishes the span for (*Client).ListVolumes.
func AfterListVolumes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeMarkEventsSeen instruments (*Client).MarkEventsSeen.
func BeforeMarkEventsSeen(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterMarkEventsSeen finishes the span for (*Client).MarkEventsSeen.
func AfterMarkEventsSeen(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeMigrateInstance instruments (*Client).MigrateInstance.
func BeforeMigrateInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterMigrateInstance finishes the span for (*Client).MigrateInstance.
func AfterMigrateInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeModifyObjectStorageBucketAccess instruments (*Client).ModifyObjectStorageBucketAccess.
func BeforeModifyObjectStorageBucketAccess(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterModifyObjectStorageBucketAccess finishes the span for (*Client).ModifyObjectStorageBucketAccess.
func AfterModifyObjectStorageBucketAccess(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforePasswordResetInstanceDisk instruments (*Client).PasswordResetInstanceDisk.
func BeforePasswordResetInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterPasswordResetInstanceDisk finishes the span for (*Client).PasswordResetInstanceDisk.
func AfterPasswordResetInstanceDisk(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforePatchMySQLDatabase instruments (*Client).PatchMySQLDatabase.
func BeforePatchMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterPatchMySQLDatabase finishes the span for (*Client).PatchMySQLDatabase.
func AfterPatchMySQLDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforePatchPostgresDatabase instruments (*Client).PatchPostgresDatabase.
func BeforePatchPostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterPatchPostgresDatabase finishes the span for (*Client).PatchPostgresDatabase.
func AfterPatchPostgresDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRebootInstance instruments (*Client).RebootInstance.
func BeforeRebootInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRebootInstance finishes the span for (*Client).RebootInstance.
func AfterRebootInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRebuildInstance instruments (*Client).RebuildInstance.
func BeforeRebuildInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRebuildInstance finishes the span for (*Client).RebuildInstance.
func AfterRebuildInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRebuildNodeBalancerConfig instruments (*Client).RebuildNodeBalancerConfig.
func BeforeRebuildNodeBalancerConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterRebuildNodeBalancerConfig finishes the span for (*Client).RebuildNodeBalancerConfig.
func AfterRebuildNodeBalancerConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRecycleLKEClusterNodes instruments (*Client).RecycleLKEClusterNodes.
func BeforeRecycleLKEClusterNodes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterRecycleLKEClusterNodes finishes the span for (*Client).RecycleLKEClusterNodes.
func AfterRecycleLKEClusterNodes(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRecycleLKENodePool instruments (*Client).RecycleLKENodePool.
func BeforeRecycleLKENodePool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRecycleLKENodePool finishes the span for (*Client).RecycleLKENodePool.
func AfterRecycleLKENodePool(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRecycleLKENodePoolNode instruments (*Client).RecycleLKENodePoolNode.
func BeforeRecycleLKENodePoolNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRecycleLKENodePoolNode finishes the span for (*Client).RecycleLKENodePoolNode.
func AfterRecycleLKENodePoolNode(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRegenerateLKECluster instruments (*Client).RegenerateLKECluster.
func BeforeRegenerateLKECluster(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRegenerateLKECluster finishes the span for (*Client).RegenerateLKECluster.
func AfterRegenerateLKECluster(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRenameInstance instruments (*Client).RenameInstance.
func BeforeRenameInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRenameInstance finishes the span for (*Client).RenameInstance.
func AfterRenameInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRenameInstanceConfig instruments (*Client).RenameInstanceConfig.
func BeforeRenameInstanceConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterRenameInstanceConfig finishes the span for (*Client).RenameInstanceConfig.
func AfterRenameInstanceConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRenameInstanceDisk instruments (*Client).RenameInstanceDisk.
func BeforeRenameInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterRenameInstanceDisk finishes the span for (*Client).RenameInstanceDisk.
func AfterRenameInstanceDisk(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeReorderInstanceConfigInterfaces instruments (*Client).ReorderInstanceConfigInterfaces.
func BeforeReorderInstanceConfigInterfaces(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterReorderInstanceConfigInterfaces finishes the span for (*Client).ReorderInstanceConfigInterfaces.
func AfterReorderInstanceConfigInterfaces(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeReplicateImage instruments (*Client).ReplicateImage.
func BeforeReplicateImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterReplicateImage finishes the span for (*Client).ReplicateImage.
func AfterReplicateImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRequestAccountServiceTransfer instruments (*Client).RequestAccountServiceTransfer.
func BeforeRequestAccountServiceTransfer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterRequestAccountServiceTransfer finishes the span for (*Client).RequestAccountServiceTransfer.
func AfterRequestAccountServiceTransfer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeRescueInstance instruments (*Client).RescueInstance.
func BeforeRescueInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterRescueInstance finishes the span for (*Client).RescueInstance.
func AfterRescueInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeReserveIPAddress instruments (*Client).ReserveIPAddress.
func BeforeReserveIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterReserveIPAddress finishes the span for (*Client).ReserveIPAddress.
func AfterReserveIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeResetInstancePassword instruments (*Client).ResetInstancePassword.
func BeforeResetInstancePassword(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterResetInstancePassword finishes the span for (*Client).ResetInstancePassword.
func AfterResetInstancePassword(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResetMySQLDatabaseCredentials instruments (*Client).ResetMySQLDatabaseCredentials.
func BeforeResetMySQLDatabaseCredentials(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterResetMySQLDatabaseCredentials finishes the span for (*Client).ResetMySQLDatabaseCredentials.
func AfterResetMySQLDatabaseCredentials(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResetOAuthClientSecret instruments (*Client).ResetOAuthClientSecret.
func BeforeResetOAuthClientSecret(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterResetOAuthClientSecret finishes the span for (*Client).ResetOAuthClientSecret.
func AfterResetOAuthClientSecret(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeResetPostgresDatabaseCredentials instruments (*Client).ResetPostgresDatabaseCredentials.
func BeforeResetPostgresDatabaseCredentials(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterResetPostgresDatabaseCredentials finishes the span for (*Client).ResetPostgresDatabaseCredentials.
func AfterResetPostgresDatabaseCredentials(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResizeInstance instruments (*Client).ResizeInstance.
func BeforeResizeInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterResizeInstance finishes the span for (*Client).ResizeInstance.
func AfterResizeInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResizeInstanceDisk instruments (*Client).ResizeInstanceDisk.
func BeforeResizeInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterResizeInstanceDisk finishes the span for (*Client).ResizeInstanceDisk.
func AfterResizeInstanceDisk(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResizeVolume instruments (*Client).ResizeVolume.
func BeforeResizeVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterResizeVolume finishes the span for (*Client).ResizeVolume.
func AfterResizeVolume(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeRestoreInstanceBackup instruments (*Client).RestoreInstanceBackup.
func BeforeRestoreInstanceBackup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterRestoreInstanceBackup finishes the span for (*Client).RestoreInstanceBackup.
func AfterRestoreInstanceBackup(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResumeMySQLDatabase instruments (*Client).ResumeMySQLDatabase.
func BeforeResumeMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterResumeMySQLDatabase finishes the span for (*Client).ResumeMySQLDatabase.
func AfterResumeMySQLDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeResumePostgresDatabase instruments (*Client).ResumePostgresDatabase.
func BeforeResumePostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterResumePostgresDatabase finishes the span for (*Client).ResumePostgresDatabase.
func AfterResumePostgresDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeSecurityQuestionsAnswer instruments (*Client).SecurityQuestionsAnswer.
func BeforeSecurityQuestionsAnswer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterSecurityQuestionsAnswer finishes the span for (*Client).SecurityQuestionsAnswer.
func AfterSecurityQuestionsAnswer(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeSecurityQuestionsList instruments (*Client).SecurityQuestionsList.
func BeforeSecurityQuestionsList(ictx hook.HookContext, recv interface{}, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// AfterSecurityQuestionsList finishes the span for (*Client).SecurityQuestionsList.
func AfterSecurityQuestionsList(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeSendPhoneNumberVerificationCode instruments (*Client).SendPhoneNumberVerificationCode.
func BeforeSendPhoneNumberVerificationCode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterSendPhoneNumberVerificationCode finishes the span for (*Client).SendPhoneNumberVerificationCode.
func AfterSendPhoneNumberVerificationCode(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeSetDefaultPaymentMethod instruments (*Client).SetDefaultPaymentMethod.
func BeforeSetDefaultPaymentMethod(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterSetDefaultPaymentMethod finishes the span for (*Client).SetDefaultPaymentMethod.
func AfterSetDefaultPaymentMethod(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeShareIPAddresses instruments (*Client).ShareIPAddresses.
func BeforeShareIPAddresses(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterShareIPAddresses finishes the span for (*Client).ShareIPAddresses.
func AfterShareIPAddresses(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeShutdownInstance instruments (*Client).ShutdownInstance.
func BeforeShutdownInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterShutdownInstance finishes the span for (*Client).ShutdownInstance.
func AfterShutdownInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeSuspendMySQLDatabase instruments (*Client).SuspendMySQLDatabase.
func BeforeSuspendMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterSuspendMySQLDatabase finishes the span for (*Client).SuspendMySQLDatabase.
func AfterSuspendMySQLDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeSuspendPostgresDatabase instruments (*Client).SuspendPostgresDatabase.
func BeforeSuspendPostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterSuspendPostgresDatabase finishes the span for (*Client).SuspendPostgresDatabase.
func AfterSuspendPostgresDatabase(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeUnassignPlacementGroupLinodes instruments (*Client).UnassignPlacementGroupLinodes.
func BeforeUnassignPlacementGroupLinodes(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUnassignPlacementGroupLinodes finishes the span for (*Client).UnassignPlacementGroupLinodes.
func AfterUnassignPlacementGroupLinodes(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateAccount instruments (*Client).UpdateAccount.
func BeforeUpdateAccount(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateAccount finishes the span for (*Client).UpdateAccount.
func AfterUpdateAccount(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateAccountSettings instruments (*Client).UpdateAccountSettings.
func BeforeUpdateAccountSettings(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateAccountSettings finishes the span for (*Client).UpdateAccountSettings.
func AfterUpdateAccountSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateDomain instruments (*Client).UpdateDomain.
func BeforeUpdateDomain(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateDomain finishes the span for (*Client).UpdateDomain.
func AfterUpdateDomain(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateDomainRecord instruments (*Client).UpdateDomainRecord.
func BeforeUpdateDomainRecord(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateDomainRecord finishes the span for (*Client).UpdateDomainRecord.
func AfterUpdateDomainRecord(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateFirewall instruments (*Client).UpdateFirewall.
func BeforeUpdateFirewall(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateFirewall finishes the span for (*Client).UpdateFirewall.
func AfterUpdateFirewall(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateFirewallRuleSet instruments (*Client).UpdateFirewallRuleSet.
func BeforeUpdateFirewallRuleSet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateFirewallRuleSet finishes the span for (*Client).UpdateFirewallRuleSet.
func AfterUpdateFirewallRuleSet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateFirewallRules instruments (*Client).UpdateFirewallRules.
func BeforeUpdateFirewallRules(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateFirewallRules finishes the span for (*Client).UpdateFirewallRules.
func AfterUpdateFirewallRules(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateFirewallSettings instruments (*Client).UpdateFirewallSettings.
func BeforeUpdateFirewallSettings(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateFirewallSettings finishes the span for (*Client).UpdateFirewallSettings.
func AfterUpdateFirewallSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateIPAddress instruments (*Client).UpdateIPAddress.
func BeforeUpdateIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateIPAddress finishes the span for (*Client).UpdateIPAddress.
func AfterUpdateIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateImage instruments (*Client).UpdateImage.
func BeforeUpdateImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateImage finishes the span for (*Client).UpdateImage.
func AfterUpdateImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateImageShareGroup instruments (*Client).UpdateImageShareGroup.
func BeforeUpdateImageShareGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateImageShareGroup finishes the span for (*Client).UpdateImageShareGroup.
func AfterUpdateImageShareGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstance instruments (*Client).UpdateInstance.
func BeforeUpdateInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateInstance finishes the span for (*Client).UpdateInstance.
func AfterUpdateInstance(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstanceConfig instruments (*Client).UpdateInstanceConfig.
func BeforeUpdateInstanceConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateInstanceConfig finishes the span for (*Client).UpdateInstanceConfig.
func AfterUpdateInstanceConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstanceConfigInterface instruments (*Client).UpdateInstanceConfigInterface.
func BeforeUpdateInstanceConfigInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}, p4 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3, p4)
}

// AfterUpdateInstanceConfigInterface finishes the span for (*Client).UpdateInstanceConfigInterface.
func AfterUpdateInstanceConfigInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstanceDisk instruments (*Client).UpdateInstanceDisk.
func BeforeUpdateInstanceDisk(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateInstanceDisk finishes the span for (*Client).UpdateInstanceDisk.
func AfterUpdateInstanceDisk(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstanceFirewalls instruments (*Client).UpdateInstanceFirewalls.
func BeforeUpdateInstanceFirewalls(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateInstanceFirewalls finishes the span for (*Client).UpdateInstanceFirewalls.
func AfterUpdateInstanceFirewalls(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInstanceIPAddress instruments (*Client).UpdateInstanceIPAddress.
func BeforeUpdateInstanceIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateInstanceIPAddress finishes the span for (*Client).UpdateInstanceIPAddress.
func AfterUpdateInstanceIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInterface instruments (*Client).UpdateInterface.
func BeforeUpdateInterface(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateInterface finishes the span for (*Client).UpdateInterface.
func AfterUpdateInterface(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateInterfaceSettings instruments (*Client).UpdateInterfaceSettings.
func BeforeUpdateInterfaceSettings(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateInterfaceSettings finishes the span for (*Client).UpdateInterfaceSettings.
func AfterUpdateInterfaceSettings(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLKECluster instruments (*Client).UpdateLKECluster.
func BeforeUpdateLKECluster(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateLKECluster finishes the span for (*Client).UpdateLKECluster.
func AfterUpdateLKECluster(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLKEClusterControlPlaneACL instruments (*Client).UpdateLKEClusterControlPlaneACL.
func BeforeUpdateLKEClusterControlPlaneACL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateLKEClusterControlPlaneACL finishes the span for (*Client).UpdateLKEClusterControlPlaneACL.
func AfterUpdateLKEClusterControlPlaneACL(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLKENodePool instruments (*Client).UpdateLKENodePool.
func BeforeUpdateLKENodePool(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateLKENodePool finishes the span for (*Client).UpdateLKENodePool.
func AfterUpdateLKENodePool(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLogStream instruments (*Client).UpdateLogStream.
func BeforeUpdateLogStream(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateLogStream finishes the span for (*Client).UpdateLogStream.
func AfterUpdateLogStream(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLogsDestination instruments (*Client).UpdateLogsDestination.
func BeforeUpdateLogsDestination(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateLogsDestination finishes the span for (*Client).UpdateLogsDestination.
func AfterUpdateLogsDestination(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLongviewClient instruments (*Client).UpdateLongviewClient.
func BeforeUpdateLongviewClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateLongviewClient finishes the span for (*Client).UpdateLongviewClient.
func AfterUpdateLongviewClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateLongviewPlan instruments (*Client).UpdateLongviewPlan.
func BeforeUpdateLongviewPlan(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateLongviewPlan finishes the span for (*Client).UpdateLongviewPlan.
func AfterUpdateLongviewPlan(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateMonitorAlertDefinition instruments (*Client).UpdateMonitorAlertDefinition.
func BeforeUpdateMonitorAlertDefinition(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateMonitorAlertDefinition finishes the span for (*Client).UpdateMonitorAlertDefinition.
func AfterUpdateMonitorAlertDefinition(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateMySQLDatabase instruments (*Client).UpdateMySQLDatabase.
func BeforeUpdateMySQLDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateMySQLDatabase finishes the span for (*Client).UpdateMySQLDatabase.
func AfterUpdateMySQLDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateNodeBalancer instruments (*Client).UpdateNodeBalancer.
func BeforeUpdateNodeBalancer(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateNodeBalancer finishes the span for (*Client).UpdateNodeBalancer.
func AfterUpdateNodeBalancer(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateNodeBalancerConfig instruments (*Client).UpdateNodeBalancerConfig.
func BeforeUpdateNodeBalancerConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateNodeBalancerConfig finishes the span for (*Client).UpdateNodeBalancerConfig.
func AfterUpdateNodeBalancerConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateNodeBalancerNode instruments (*Client).UpdateNodeBalancerNode.
func BeforeUpdateNodeBalancerNode(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}, p4 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3, p4)
}

// AfterUpdateNodeBalancerNode finishes the span for (*Client).UpdateNodeBalancerNode.
func AfterUpdateNodeBalancerNode(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateOAuthClient instruments (*Client).UpdateOAuthClient.
func BeforeUpdateOAuthClient(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateOAuthClient finishes the span for (*Client).UpdateOAuthClient.
func AfterUpdateOAuthClient(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateObjectStorageBucketAccess instruments (*Client).UpdateObjectStorageBucketAccess.
func BeforeUpdateObjectStorageBucketAccess(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateObjectStorageBucketAccess finishes the span for (*Client).UpdateObjectStorageBucketAccess.
func AfterUpdateObjectStorageBucketAccess(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeUpdateObjectStorageKey instruments (*Client).UpdateObjectStorageKey.
func BeforeUpdateObjectStorageKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateObjectStorageKey finishes the span for (*Client).UpdateObjectStorageKey.
func AfterUpdateObjectStorageKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateObjectStorageObjectACLConfig instruments (*Client).UpdateObjectStorageObjectACLConfig.
func BeforeUpdateObjectStorageObjectACLConfig(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateObjectStorageObjectACLConfig finishes the span for (*Client).UpdateObjectStorageObjectACLConfig.
func AfterUpdateObjectStorageObjectACLConfig(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdatePlacementGroup instruments (*Client).UpdatePlacementGroup.
func BeforeUpdatePlacementGroup(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdatePlacementGroup finishes the span for (*Client).UpdatePlacementGroup.
func AfterUpdatePlacementGroup(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdatePostgresDatabase instruments (*Client).UpdatePostgresDatabase.
func BeforeUpdatePostgresDatabase(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdatePostgresDatabase finishes the span for (*Client).UpdatePostgresDatabase.
func AfterUpdatePostgresDatabase(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateProfile instruments (*Client).UpdateProfile.
func BeforeUpdateProfile(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateProfile finishes the span for (*Client).UpdateProfile.
func AfterUpdateProfile(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateProfilePreferences instruments (*Client).UpdateProfilePreferences.
func BeforeUpdateProfilePreferences(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUpdateProfilePreferences finishes the span for (*Client).UpdateProfilePreferences.
func AfterUpdateProfilePreferences(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateReservedIPAddress instruments (*Client).UpdateReservedIPAddress.
func BeforeUpdateReservedIPAddress(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateReservedIPAddress finishes the span for (*Client).UpdateReservedIPAddress.
func AfterUpdateReservedIPAddress(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateSSHKey instruments (*Client).UpdateSSHKey.
func BeforeUpdateSSHKey(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateSSHKey finishes the span for (*Client).UpdateSSHKey.
func AfterUpdateSSHKey(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateStackscript instruments (*Client).UpdateStackscript.
func BeforeUpdateStackscript(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateStackscript finishes the span for (*Client).UpdateStackscript.
func AfterUpdateStackscript(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateToken instruments (*Client).UpdateToken.
func BeforeUpdateToken(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateToken finishes the span for (*Client).UpdateToken.
func AfterUpdateToken(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateUser instruments (*Client).UpdateUser.
func BeforeUpdateUser(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateUser finishes the span for (*Client).UpdateUser.
func AfterUpdateUser(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateUserGrants instruments (*Client).UpdateUserGrants.
func BeforeUpdateUserGrants(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateUserGrants finishes the span for (*Client).UpdateUserGrants.
func AfterUpdateUserGrants(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateUserRolePermissions instruments (*Client).UpdateUserRolePermissions.
func BeforeUpdateUserRolePermissions(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateUserRolePermissions finishes the span for (*Client).UpdateUserRolePermissions.
func AfterUpdateUserRolePermissions(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateVPC instruments (*Client).UpdateVPC.
func BeforeUpdateVPC(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateVPC finishes the span for (*Client).UpdateVPC.
func AfterUpdateVPC(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateVPCSubnet instruments (*Client).UpdateVPCSubnet.
func BeforeUpdateVPCSubnet(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUpdateVPCSubnet finishes the span for (*Client).UpdateVPCSubnet.
func AfterUpdateVPCSubnet(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpdateVolume instruments (*Client).UpdateVolume.
func BeforeUpdateVolume(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpdateVolume finishes the span for (*Client).UpdateVolume.
func AfterUpdateVolume(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUpgradeInstance instruments (*Client).UpgradeInstance.
func BeforeUpgradeInstance(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpgradeInstance finishes the span for (*Client).UpgradeInstance.
func AfterUpgradeInstance(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeUpgradeInterfaces instruments (*Client).UpgradeInterfaces.
func BeforeUpgradeInterfaces(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUpgradeInterfaces finishes the span for (*Client).UpgradeInterfaces.
func AfterUpgradeInterfaces(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUploadImage instruments (*Client).UploadImage.
func BeforeUploadImage(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterUploadImage finishes the span for (*Client).UploadImage.
func AfterUploadImage(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeUploadImageToURL instruments (*Client).UploadImageToURL.
func BeforeUploadImageToURL(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// AfterUploadImageToURL finishes the span for (*Client).UploadImageToURL.
func AfterUploadImageToURL(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// BeforeUploadObjectStorageBucketCert instruments (*Client).UploadObjectStorageBucketCert.
func BeforeUploadObjectStorageBucketCert(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}, p2 interface{}, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// AfterUploadObjectStorageBucketCert finishes the span for (*Client).UploadObjectStorageBucketCert.
func AfterUploadObjectStorageBucketCert(ictx hook.HookContext, r0 interface{}, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// BeforeVerifyPhoneNumber instruments (*Client).VerifyPhoneNumber.
func BeforeVerifyPhoneNumber(ictx hook.HookContext, recv interface{}, p0 interface{}, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// AfterVerifyPhoneNumber finishes the span for (*Client).VerifyPhoneNumber.
func AfterVerifyPhoneNumber(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}
