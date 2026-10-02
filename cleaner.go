//go:build windows

// cleaner.go — memory reduction engine, a faithful Go port of the cleaning
// operations performed by memreduct (https://github.com/henrypp/memreduct).
//
// memreduct applies its "reduction mask" through NtSetSystemInformation:
//   - working set trimming          (SystemMemoryListInformation)
//   - system file cache trim        (SystemFileCacheInformationEx)
//   - volume cache flush            (FindFirstVolume + FlushFileBuffers)
//   - modified page list flush      (SystemMemoryListInformation)
//   - standby list purge            (SystemMemoryListInformation)
//   - priority-0 standby purge      (SystemMemoryListInformation)
//   - registry cache flush          (SystemRegistryReconciliationInformation)
//   - physical page combine         (SystemCombinePhysicalMemoryInformation)
package main

import (
	"fmt"
	"unsafe"
)

// reduction mask bits (identical to memreduct's REDUCT_* flags)
const (
	maskWorkingSet          = 0x01 // MemoryEmptyWorkingSets
	maskSystemFileCache     = 0x02 // SystemFileCacheInformationEx
	maskStandbyPriority0    = 0x04 // MemoryPurgeLowPriorityStandbyList
	maskStandbyList         = 0x08 // MemoryPurgeStandbyList
	maskModifiedList        = 0x10 // MemoryFlushModifiedList
	maskCombineMemoryLists  = 0x20 // SystemCombinePhysicalMemoryInformation
	maskRegistryCache       = 0x40 // SystemRegistryReconciliationInformation
	maskModifiedFileCache   = 0x80 // flush volume caches

	// memreduct's default mask (no full standby purge, no modified-list flush)
	maskStandard = maskWorkingSet | maskSystemFileCache | maskStandbyPriority0 |
		maskRegistryCache | maskCombineMemoryLists | maskModifiedFileCache

	// everything, including the "freeze" operations (may briefly stall apps)
	maskDeep = maskStandard | maskStandbyList | maskModifiedList
)

// SYSTEM_INFORMATION_CLASS values used by memreduct
const (
	sysMemoryListInformation       = 80  // 0x50
	sysFileCacheInformation        = 28  // classic class; the "Ex" (81) is rejected on Win11 26200+
	sysRegistryReconciliationInfo  = 155 // 0x9B
	sysCombinePhysicalMemoryInfo   = 229 // SystemMemoryCombineInformation
)

// SYSTEM_MEMORY_LIST_COMMAND values
const (
	memEmptyWorkingSets              = 2
	memFlushModifiedList             = 3
	memPurgeStandbyList              = 4
	memPurgeLowPriorityStandbyList   = 5
)

type cleanResult struct {
	Freed uint64
	Errs  int
}

// enableCleanerPrivileges enables the privileges memreduct relies on.
// SeProfileSingleProcessPrivilege -> SystemMemoryListInformation commands
// SeIncreaseQuotaPrivilege        -> SystemFileCacheInformationEx
func enableCleanerPrivileges() {
	enablePrivilege("SeProfileSingleProcessPrivilege")
	enablePrivilege("SeIncreaseQuotaPrivilege")
}

// cleanMemory runs the requested reduction mask and returns the number of
// physical bytes that became available in the process.
func cleanMemory(mask uint32) cleanResult {
	_, beforeAvail, _ := memoryInfo()
	errs := 0

	memList := func(command uintptr) {
		if st := ntSetSystemInformation(sysMemoryListInformation,
			unsafe.Pointer(&command), unsafe.Sizeof(uint32(0))); st != 0 {
			errs++
		}
	}

	// Working set (vista+)
	if mask&maskWorkingSet != 0 {
		memList(memEmptyWorkingSets)
	}

	// System file cache — classic SystemFileCacheInformation class with a
	// 16-byte { MinimumWorkingSet, MaximumWorkingSet } pair of SIZE_MAX,
	// which flushes the file cache (the Ex class is not accepted on
	// Windows 11 26200+; verified empirically)
	if mask&maskSystemFileCache != 0 {
		info := [2]uintptr{^uintptr(0), ^uintptr(0)}
		if st := ntSetSystemInformation(sysFileCacheInformation,
			unsafe.Pointer(&info[0]), unsafe.Sizeof(info)); st != 0 {
			errs++
		}
	}

	// Flush volume cache
	if mask&maskModifiedFileCache != 0 {
		flushVolumeCache()
	}

	// Modified page list (vista+)
	if mask&maskModifiedList != 0 {
		memList(memFlushModifiedList)
	}

	// Standby list (vista+)
	if mask&maskStandbyList != 0 {
		memList(memPurgeStandbyList)
	}

	// Standby priority-0 list (vista+)
	if mask&maskStandbyPriority0 != 0 {
		memList(memPurgeLowPriorityStandbyList)
	}

	// Flush registry cache (win8.1+)
	if mask&maskRegistryCache != 0 {
		if st := ntSetSystemInformation(sysRegistryReconciliationInfo, nil, 0); st != 0 {
			errs++
		}
	}

	// Combine memory lists (win8.1+) — best-effort: modern Windows 11 builds
	// reject this operation with STATUS_INVALID_HANDLE regardless of
	// parameters (verified empirically; memreduct only logs the error), so
	// failures here must not count toward the user-visible error state.
	if mask&maskCombineMemoryLists != 0 {
		var combineInfo [4]uintptr // MEMORY_COMBINE_INFORMATION_EX (zeroed)
		ntSetSystemInformation(sysCombinePhysicalMemoryInfo,
			unsafe.Pointer(&combineInfo[0]), unsafe.Sizeof(combineInfo))
	}

	// difference (after) — same accounting as memreduct: usage delta
	_, afterAvail, _ := memoryInfo()
	res := cleanResult{Errs: errs}
	if afterAvail > beforeAvail {
		res.Freed = afterAvail - beforeAvail
	}
	return res
}

// flushVolumeCache opens every volume and flushes its write cache.
// Best-effort; silently ignores volumes that cannot be opened.
func flushVolumeCache() {
	const maxVols = 64
	buf := make([]uint16, 300)
	h, _, _ := procFindFirstVolumeW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if h == 0 || h == invalidHandleValue {
		return
	}
	defer procFindVolumeClose.Call(h)

	for i := 0; i < maxVols; i++ {
		// Open with GENERIC_WRITE|FILE_SHARE_READ|FILE_SHARE_WRITE, OPEN_EXISTING.
		hv, _, _ := procCreateFileW.Call(uintptr(unsafe.Pointer(&buf[0])),
			0x40000000, 0x00000003, 0, 3, 0, 0)
		if hv != 0 && hv != invalidHandleValue {
			procFlushFileBuffers.Call(hv)
			procCloseHandle.Call(hv)
		}
		r, _, _ := procFindNextVolumeW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			break
		}
	}
}

// humanBytes formats a byte count the way memreduct does (1 decimal place).
func humanBytes(b uint64) string {
	const unit = 1024.0
	f := float64(b)
	exp := 0
	for f >= unit && exp < 5 {
		f /= unit
		exp++
	}
	switch exp {
	case 0:
		return fmt.Sprintf("%.0f B", f)
	case 1:
		return fmt.Sprintf("%.1f KB", f)
	case 2:
		return fmt.Sprintf("%.1f MB", f)
	case 3:
		return fmt.Sprintf("%.1f GB", f)
	case 4:
		return fmt.Sprintf("%.1f TB", f)
	default:
		return fmt.Sprintf("%.1f PB", f)
	}
}
