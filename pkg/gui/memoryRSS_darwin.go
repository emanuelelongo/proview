//go:build darwin

package gui

/*
#include <mach/mach.h>

static uint64_t proviewResidentBytes(void) {
	mach_task_basic_info_data_t info;
	mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
	if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return info.resident_size;
}
*/
import "C"

import "errors"

func processResidentBytes() (uint64, error) {
	bytes := uint64(C.proviewResidentBytes())
	if bytes == 0 {
		return 0, errors.New("mach task_info did not return resident memory")
	}
	return bytes, nil
}
