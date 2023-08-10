Limitations
------------------------------------

Session Labeling
~~~~~~~~~~~~~~~~
Cloud Spanner Session Labeling is not supported.

Request Priority
~~~~~~~~~~~~~~~~
Request priority can be set by unwrapping the Spanner-specific `SpannerConn` interface and setting the request priority as part of a db call.

Tagging
~~~~~~~
Tags can be set by unwrapping the Spanner-specific `SpannerConn` interface and setting the tags using that interface.

Partition Reads
~~~~~~~
Partition Reads can be done by unwrapping the Spanner-specific `SpannerConn` interface and doing the parition reads using that interface.

PostgreSQL
~~~~~~~
Spanner databases that use the PostgreSQL dialect are not yet supported. Check [Integrate Spanner PostgreSQL-dialect with GORM](https://cloud.google.com/spanner/docs/use-gorm#install) to use GORM with PG Spanner.

Backups
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
Backups are not supported by this driver. Use the [Cloud Spanner Go client library](https://github.com/googleapis/google-cloud-go/tree/main/spanner) to manage backups programmatically.
