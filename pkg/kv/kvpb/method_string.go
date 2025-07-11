// Copyright 2023 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

// Code generated by "stringer"; DO NOT EDIT.

package kvpb

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Get-0]
	_ = x[Put-1]
	_ = x[ConditionalPut-2]
	_ = x[Increment-3]
	_ = x[Delete-4]
	_ = x[DeleteRange-5]
	_ = x[ClearRange-6]
	_ = x[RevertRange-7]
	_ = x[Scan-8]
	_ = x[ReverseScan-9]
	_ = x[EndTxn-10]
	_ = x[AdminSplit-11]
	_ = x[AdminUnsplit-12]
	_ = x[AdminMerge-13]
	_ = x[AdminTransferLease-14]
	_ = x[AdminChangeReplicas-15]
	_ = x[AdminRelocateRange-16]
	_ = x[HeartbeatTxn-17]
	_ = x[GC-18]
	_ = x[PushTxn-19]
	_ = x[RecoverTxn-20]
	_ = x[QueryLocks-21]
	_ = x[QueryTxn-22]
	_ = x[QueryIntent-23]
	_ = x[ResolveIntent-24]
	_ = x[ResolveIntentRange-25]
	_ = x[Merge-26]
	_ = x[TruncateLog-27]
	_ = x[RequestLease-28]
	_ = x[TransferLease-29]
	_ = x[LeaseInfo-30]
	_ = x[ComputeChecksum-31]
	_ = x[CheckConsistency-32]
	_ = x[WriteBatch-33]
	_ = x[Export-34]
	_ = x[AdminScatter-35]
	_ = x[AddSSTable-36]
	_ = x[LinkExternalSSTable-37]
	_ = x[Migrate-38]
	_ = x[RecomputeStats-39]
	_ = x[Refresh-40]
	_ = x[RefreshRange-41]
	_ = x[Subsume-42]
	_ = x[RangeStats-43]
	_ = x[QueryResolvedTimestamp-44]
	_ = x[Barrier-45]
	_ = x[Probe-46]
	_ = x[IsSpanEmpty-47]
	_ = x[Excise-48]
	_ = x[FlushLockTable-49]
	_ = x[MaxMethod-49]
	_ = x[NumMethods-50]
}

func (i Method) String() string {
	switch i {
	case Get:
		return "Get"
	case Put:
		return "Put"
	case ConditionalPut:
		return "ConditionalPut"
	case Increment:
		return "Increment"
	case Delete:
		return "Delete"
	case DeleteRange:
		return "DeleteRange"
	case ClearRange:
		return "ClearRange"
	case RevertRange:
		return "RevertRange"
	case Scan:
		return "Scan"
	case ReverseScan:
		return "ReverseScan"
	case EndTxn:
		return "EndTxn"
	case AdminSplit:
		return "AdminSplit"
	case AdminUnsplit:
		return "AdminUnsplit"
	case AdminMerge:
		return "AdminMerge"
	case AdminTransferLease:
		return "AdminTransferLease"
	case AdminChangeReplicas:
		return "AdminChangeReplicas"
	case AdminRelocateRange:
		return "AdminRelocateRange"
	case HeartbeatTxn:
		return "HeartbeatTxn"
	case GC:
		return "GC"
	case PushTxn:
		return "PushTxn"
	case RecoverTxn:
		return "RecoverTxn"
	case QueryLocks:
		return "QueryLocks"
	case QueryTxn:
		return "QueryTxn"
	case QueryIntent:
		return "QueryIntent"
	case ResolveIntent:
		return "ResolveIntent"
	case ResolveIntentRange:
		return "ResolveIntentRange"
	case Merge:
		return "Merge"
	case TruncateLog:
		return "TruncateLog"
	case RequestLease:
		return "RequestLease"
	case TransferLease:
		return "TransferLease"
	case LeaseInfo:
		return "LeaseInfo"
	case ComputeChecksum:
		return "ComputeChecksum"
	case CheckConsistency:
		return "CheckConsistency"
	case WriteBatch:
		return "WriteBatch"
	case Export:
		return "Export"
	case AdminScatter:
		return "AdminScatter"
	case AddSSTable:
		return "AddSSTable"
	case LinkExternalSSTable:
		return "LinkExternalSSTable"
	case Migrate:
		return "Migrate"
	case RecomputeStats:
		return "RecomputeStats"
	case Refresh:
		return "Refresh"
	case RefreshRange:
		return "RefreshRange"
	case Subsume:
		return "Subsume"
	case RangeStats:
		return "RangeStats"
	case QueryResolvedTimestamp:
		return "QueryResolvedTimestamp"
	case Barrier:
		return "Barrier"
	case Probe:
		return "Probe"
	case IsSpanEmpty:
		return "IsSpanEmpty"
	case Excise:
		return "Excise"
	case FlushLockTable:
		return "FlushLockTable"
	case NumMethods:
		return "NumMethods"
	default:
		return "Method(" + strconv.FormatInt(int64(i), 10) + ")"
	}
}

var StringToMethodMap = map[string]Method{
	"Get":                    0,
	"Put":                    1,
	"ConditionalPut":         2,
	"Increment":              3,
	"Delete":                 4,
	"DeleteRange":            5,
	"ClearRange":             6,
	"RevertRange":            7,
	"Scan":                   8,
	"ReverseScan":            9,
	"EndTxn":                 10,
	"AdminSplit":             11,
	"AdminUnsplit":           12,
	"AdminMerge":             13,
	"AdminTransferLease":     14,
	"AdminChangeReplicas":    15,
	"AdminRelocateRange":     16,
	"HeartbeatTxn":           17,
	"GC":                     18,
	"PushTxn":                19,
	"RecoverTxn":             20,
	"QueryLocks":             21,
	"QueryTxn":               22,
	"QueryIntent":            23,
	"ResolveIntent":          24,
	"ResolveIntentRange":     25,
	"Merge":                  26,
	"TruncateLog":            27,
	"RequestLease":           28,
	"TransferLease":          29,
	"LeaseInfo":              30,
	"ComputeChecksum":        31,
	"CheckConsistency":       32,
	"WriteBatch":             33,
	"Export":                 34,
	"AdminScatter":           35,
	"AddSSTable":             36,
	"LinkExternalSSTable":    37,
	"Migrate":                38,
	"RecomputeStats":         39,
	"Refresh":                40,
	"RefreshRange":           41,
	"Subsume":                42,
	"RangeStats":             43,
	"QueryResolvedTimestamp": 44,
	"Barrier":                45,
	"Probe":                  46,
	"IsSpanEmpty":            47,
	"Excise":                 48,
	"FlushLockTable":         49,
	"MaxMethod":              49,
	"NumMethods":             50,
}
