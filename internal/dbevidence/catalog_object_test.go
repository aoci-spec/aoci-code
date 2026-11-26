package dbevidence

import (
	"database/sql/driver"
	"testing"
)

func TestOpenGaussCatalogObjectDisplay(t *testing.T) {
	for _, item := range []struct{ namespace, name, want string }{
		{"public", "客户视图", "public.客户视图"},
		{"sales.region", "客户.(视图)\nactive", `"sales.region"."客户.(视图)\nactive"`},
	} {
		t.Run("view/"+item.namespace, func(t *testing.T) {
			err := assertOpenGaussCatalogObject(t, openGaussUnsupportedSQL, []driver.Value{item.namespace, item.name, "view"},
				"unsupported", "view", item.namespace+"."+item.name)
			if got := err.DisplayObject(); got != item.want {
				t.Fatalf("view display = %q, want %q", got, item.want)
			}
		})
	}
	for _, item := range []struct{ name, member, want string }{
		{"ordinary", "accounts_pk", "public.accounts (accounts_pk)"},
		{"newline", "accounts_ix)\nfixture", `public.accounts ("accounts_ix)\nfixture")`},
		{"han", "索引(账户)", `public.accounts ("索引(账户)")`},
		{"escapes", "ix.\x1b\t\"\\\u2028key", `public.accounts ("ix.\x1b\t\"\\\u2028key")`},
	} {
		t.Run("index/"+item.name, func(t *testing.T) {
			_, rows := mockOpenGaussResult(openGaussIndexesSQL)
			row := rows[0]
			row[2], row[7] = item.member, "0 2"
			err := assertOpenGaussCatalogObject(t, openGaussIndexesSQL, row, "indexes", "expression_index", "public.accounts ("+item.member+")")
			if got := err.DisplayObject(); got != item.want {
				t.Fatalf("index display = %q, want %q", got, item.want)
			}
		})
	}
}
