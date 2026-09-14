# Examples

This directory contains examples that are mostly used for documentation, but can also be run/tested manually via the Terraform CLI.

Each resource, data source and ephemeral resource has a directory holding one `.tf` file per example. The template for its documentation page in `templates/` lists which files are rendered and the heading each appears under, so adding an example means adding a file here and a `tffile` line in the matching template:

* **provider/*.tf** example files for the provider index page
* **data-sources/`full data source name`/*.tf** example files for the named data source page
* **ephemeral-resources/`full ephemeral resource name`/*.tf** example files for the named ephemeral resource page
* **resources/`full resource name`/*.tf** example files for the named resource page
* **resources/`full resource name`/import.sh** the import command for the named resource page

`full` and `iam-demo` are runnable configurations and are not rendered into the documentation.
