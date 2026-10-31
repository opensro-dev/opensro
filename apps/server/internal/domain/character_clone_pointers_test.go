/*
===========================================================================

character_clone_pointers_test.go - CloneCharacter detaches every pointer

The store's section skip compares a live record against its committed
clone, so a pointer the clone shares reads equal after an in-place write
and that save is lost. This walks the type, not a list, so a pointer
field added to Character fails here until the clone detaches it.

===========================================================================
*/

package domain

import (
	"reflect"
	"testing"
)

/*
================
fillPointers

Allocate every nil pointer reachable through structs and pointers.
================
*/
func fillPointers(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		fillPointers(value.Elem())
	case reflect.Struct:
		for i := range value.NumField() {
			if value.Field(i).CanSet() {
				fillPointers(value.Field(i))
			}
		}
	}
}

/*
================
sharedPointers

The paths at which two values of one type hold the same pointer.
================
*/
func sharedPointers(a, b reflect.Value, path string) []string {
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return nil
		}
		if a.Pointer() == b.Pointer() {
			return []string{path}
		}
		return sharedPointers(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		var shared []string
		for i := range a.NumField() {
			field := a.Type().Field(i)
			if field.IsExported() {
				shared = append(shared, sharedPointers(a.Field(i), b.Field(i), path+"."+field.Name)...)
			}
		}
		return shared
	}
	return nil
}

/*
================
TestCloneCharacterDetachesEveryPointer
================
*/
func TestCloneCharacterDetachesEveryPointer(t *testing.T) {
	source := &Character{}
	fillPointers(reflect.ValueOf(source).Elem())
	clone := CloneCharacter(source)
	if shared := sharedPointers(reflect.ValueOf(source), reflect.ValueOf(clone), "Character"); len(shared) != 0 {
		t.Fatalf("the clone aliases %v", shared)
	}
}
