package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Result reduces the status of an object to a single value.
type Result int

const (
	Unknown Result = iota
	Ok
	Warning
	Error
)

func (s Result) String() string {
	switch s {
	case Ok:
		return "Ok"
	case Warning:
		return "Warning"
	case Error:
		return "Error"
	default:
		return "Unknown"
	}
}

// Status is the core structure representing the status of an object.
type Status struct {
	Result      Result // mapping to Result enum
	Progressing bool   // true if the object is still progressing
	Status      string // human readable status
	Err         error  // error appeared during the evaluation
}

// ObjectStatus combines the object with status-related information.
type ObjectStatus struct {
	Object      *Object           // the subject of the status
	ObjStatus   Status            // overall status of the object
	SubStatuses []ObjectStatus    // statuses of the sub-objects (e.g. pods of a replicaset)
	Conditions  []ConditionStatus // conditions of the object
}

func (os ObjectStatus) Status() Status {
	return os.ObjStatus
}

type ConditionStatus struct {
	*metav1.Condition
	// CondStatus is a pointer to the underlying condition status.
	// We're using the pointer to allow modifying the status.
	CondStatus *Status
}

func (cs ConditionStatus) Status() Status {
	return *cs.CondStatus
}

func UnknownStatus(obj *Object) ObjectStatus {
	return ObjectStatus{
		Object:     obj,
		ObjStatus:  Status{Result: Unknown, Status: "Unknown"},
		Conditions: []ConditionStatus{},
	}
}

func UnknownStatusWithError(obj *Object, err error) ObjectStatus {
	return ObjectStatus{
		Object:     obj,
		ObjStatus:  Status{Result: Unknown, Status: "Unknown", Err: err},
		Conditions: []ConditionStatus{},
	}
}

func OkStatus(obj *Object, subStatuses []ObjectStatus) ObjectStatus {
	return ObjectStatus{
		Object: obj,
		ObjStatus: Status{
			Result: Ok,
			Status: Ok.String()},
		SubStatuses: subStatuses,
	}
}

// GetCondition returns the condition with the given type.
// If the condition is not found, it returns nil.
func GetCondition(conditions []ConditionStatus, condType string) *ConditionStatus {
	for _, cond := range conditions {
		if cond.Type == condType {
			return &cond
		}
	}
	return nil
}
