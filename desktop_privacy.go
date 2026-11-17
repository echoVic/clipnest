package main

import (
	"reflect"

	"github.com/egoist/mygo"
)

func (d *Desktop) Privacy() PrivacySettings { return d.storage.Privacy.Settings() }

func (d *Desktop) SavePrivacy(s PrivacySettings) (PrivacySettings, error) {
	return d.savePrivacy(s, func() {
		mygo.RunOnMain(func() { d.lastRevision = d.n.Revision() })
	})
}

// savePrivacy calls consume once the published settings change, so the copy
// already on the clipboard is skipped instead of recorded under new rules.
// Even a post-rename sync error can publish them.
func (d *Desktop) savePrivacy(s PrivacySettings, consume func()) (PrivacySettings, error) {
	before := d.storage.Privacy.Settings()
	err := d.storage.Privacy.Save(s)
	after := d.storage.Privacy.Settings()
	if !reflect.DeepEqual(before, after) {
		consume()
	}
	return after, err
}
