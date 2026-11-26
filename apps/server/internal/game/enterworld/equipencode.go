/*
===========================================================================

equipencode.go - EquipItemRow encoding for the enter-world bootstrap

Owns the recursive WireItem → EquipItemRow walk, including a Magic
Cube's linked elixir rows (#3906).

===========================================================================
*/
package enterworld

/*
================
encodeEquipItems

One inventory or avatar list as EquipItemRow values. A Magic Cube's
linked storage is encoded the same way so the browser window can draw
nested elixir rows.
================
*/
func encodeEquipItems(rows []WireItem) []EquipItemRow {
	equipItems := make([]EquipItemRow, 0, len(rows))
	for _, item := range rows {
		body := BuildItemBody(item)
		bodyInts := make([]int, len(body))
		for index, value := range body {
			bodyInts[index] = int(value)
		}
		row := EquipItemRow{
			RefObjID: item.RefObjID,
			Slot:     item.Slot,
			Body:     bodyInts,
		}
		if item.LinkedStorage != nil && item.LinkedStorage.Capacity > 0 {
			row.LinkedStorage = &LinkedStorageRow{
				Capacity: item.LinkedStorage.Capacity,
				Rows:     encodeEquipItems(InventoryWireItems(item.LinkedStorage.Rows)),
			}
		}
		equipItems = append(equipItems, row)
	}
	return equipItems
}
