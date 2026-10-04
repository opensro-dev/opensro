package enterworld

/*
================================================================================
Dependency facets

Deps is the enter-world composition object. Other lanes consume it through
small consumer-owned interfaces, so they cannot couple themselves to unrelated
bootstrap collaborators.
================================================================================
*/

// CharactersForDivision exposes only the character-source facet.
func (d *Deps) CharactersForDivision(divisionID string) []*Character {
	if d == nil || d.Characters == nil {
		return nil
	}
	return d.Characters.CharactersForDivision(divisionID)
}

// LetterAuthority exposes the persisted mailbox facet.
func (d *Deps) LetterAuthority() LetterStore {
	if d == nil {
		return nil
	}
	return d.Letters
}

// GuildAuthority exposes the persisted guild topology facet.
func (d *Deps) GuildAuthority() GuildStore {
	if d == nil {
		return nil
	}
	return d.Guilds
}

// TrainingCampAuthority exposes the persisted mentor-camp topology facet.
func (d *Deps) TrainingCampAuthority() TrainingCampStore {
	if d == nil {
		return nil
	}
	return d.TrainingCamps
}

// PlayableModel answers a roster model's codename by RefObjID.
func (d *Deps) PlayableModel(refObjID uint32) (string, bool) {
	if d == nil || d.Roster == nil {
		return "", false
	}
	model := d.Roster.ModelByRefObjID(refObjID)
	if model == nil {
		return "", false
	}
	return model.Codename, true
}

// CharacterModelRef resolves the roster-backed model reference used by social
// rows without exposing the roster implementation.
func (d *Deps) CharacterModelRef(character *Character) uint32 {
	if d == nil {
		return CharacterModelRef(character, nil)
	}
	return CharacterModelRef(character, d.Roster)
}

/*
==================
CharacterBodyRadius

CharacterBodyRadius resolves the RefObjChar BCRadius for the character's
concrete model. Combat consumes it through a narrow authority port; it is
not inferred from browser mesh bounds or body-shape presentation sliders.
==================
*/
func (d *Deps) CharacterBodyRadius(character *Character) (float64, bool) {
	if d == nil || d.Roster == nil {
		return 0, false
	}
	model := d.Roster.ModelByRefObjID(CharacterModelRef(character, d.Roster))
	if model == nil || model.BodyRadius <= 0 {
		return 0, false
	}
	return model.BodyRadius, true
}

// ItemReferences exposes itemdata lookup to item operations.
func (d *Deps) ItemReferences() ItemRefSource {
	if d == nil {
		return nil
	}
	return d.Items
}

// LevelData exposes the authored level curve and mastery costs.
func (d *Deps) LevelData() LevelDataSource {
	if d == nil {
		return nil
	}
	return d.Levels
}

// MagicOptionDefinitions exposes the magicoption.txt rows item options
// resolve through.
func (d *Deps) MagicOptionDefinitions() MagicOptionSource {
	if d == nil {
		return nil
	}
	return d.MagicOptions
}

// SkillData exposes the authored skill-learning rows.
func (d *Deps) SkillData() SkillDataSource {
	if d == nil {
		return nil
	}
	return d.Skills
}
