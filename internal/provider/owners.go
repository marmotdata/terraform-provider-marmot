// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// setStrings reads a string Set into a slice, returning nil when the set is
// unset. The values are not sorted; callers that need a stable order sort them.
func setStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

// stringsToSet builds a sorted string Set, returning a null Set when there are
// no values so an unset owner list reads back as null rather than an empty set.
func stringsToSet(ctx context.Context, vals []string, diags *diag.Diagnostics) types.Set {
	if len(vals) == 0 {
		return types.SetNull(types.StringType)
	}
	sorted := make([]string, len(vals))
	copy(sorted, vals)
	sort.Strings(sorted)

	set, d := types.SetValueFrom(ctx, types.StringType, sorted)
	diags.Append(d...)
	return set
}
