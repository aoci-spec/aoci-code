package dbevidence

import "testing"

// Names that merely start with LOCAL or GLOBAL are ordinary indexes; only the
// clause after the column list marks a partition index (audit G6-01).
func TestOpenGaussPartitionIndexDefinition(t *testing.T) {
	for definition, want := range map[string]bool{
		"CREATE UNIQUE INDEX locale_pkey ON public.locale USING btree (id) TABLESPACE pg_default":                   false,
		"CREATE UNIQUE INDEX global_settings_pkey ON global.global_settings USING btree (id) TABLESPACE pg_default": false,
		"CREATE INDEX t_idx ON public.t USING btree (id, locale) TABLESPACE pg_default":                             false,
		"CREATE INDEX t_idx ON public.t USING btree (id ASC NULLS LAST) TABLESPACE pg_default":                      false,
		"CREATE INDEX t_idx ON public.t USING btree (id) LOCAL(PARTITION p1_id_idx, PARTITION p2_id_idx)":           true,
		"CREATE INDEX t_idx ON public.t USING btree (id) TABLESPACE pg_default LOCAL(PARTITION p1_id_idx)":          true,
		"CREATE INDEX t_idx ON public.t USING btree (id) GLOBAL TABLESPACE pg_default":                              true,
	} {
		if got := openGaussPartitionIndexDefinition(definition); got != want {
			t.Errorf("%s: partition=%v, want %v", definition, got, want)
		}
	}
}

// A column vector naming one column twice fails closed; the option vector,
// where repeats are ordinary, is untouched (audit G6-07).
func TestCatalogColumnVectorRefusesARepeatedColumn(t *testing.T) {
	if _, err := catalogColumnVector("2 2", catalogObjectName{}); err == nil {
		t.Fatal("a repeated column ordinal was accepted")
	}
	if got, err := catalogColumnVector("1 3", catalogObjectName{}); err != nil || len(got) != 2 {
		t.Fatalf("distinct ordinals: %v %v", got, err)
	}
	if got, err := catalogVectorOrdinals("0 0"); err != nil || len(got) != 2 {
		t.Fatalf("an option vector with repeats: %v %v", got, err)
	}
}
