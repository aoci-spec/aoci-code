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
