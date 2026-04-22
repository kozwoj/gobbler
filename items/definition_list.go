package items

/*
	DefinitionList is a map of item type name to its ItemDefinition. It is used to store all item definitions

that are available for validating and converting input items.
*/
type DefinitionList map[string]ItemDefinition

/* AddDefinition function takes a parsed ItemDefinition and adds it to the DefinitionList. It returns an error if the definition is invalid or if a definition with the same item type name already exists in the DefinitionList.
 */
func (dl DefinitionList) AddDefinition(def ItemDefinition) error {
	if _, exists := dl[def.TypeName]; exists {
		return NewDefinitionError("name", def.TypeName, ErrDefinitionAlreadyExists)
	}
	dl[def.TypeName] = def
	return nil
}

/* GetDefinition function takes an item type name and returns the corresponding ItemDefinition from the DefinitionList. It returns an error if no definition with the given item type name exists in the DefinitionList.
 */
func (dl DefinitionList) GetDefinition(typeName string) (ItemDefinition, error) {
	def, exists := dl[typeName]
	if !exists {
		return ItemDefinition{}, NewDefinitionError("name", typeName, ErrDefinitionNotFound)
	}
	return def, nil
}
