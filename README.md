Clone graph in AgensGraph Database
==================================

[AgensGraph](https://travis-ci.org/bitnine-oss/agensgraph) is a new generation multi-model graph database for the modern complex data environment.

[Sometimes](https://stackoverflow.com/questions/56272245/how-to-copy-data-from-one-graph-to-another) it seems useful to be able to clone the whole graph into another graph in the same database.
AgensGraph have not produced such a utility. Here is a try to create such a utility in Go language.

## So, what is a Graph in AgensGraph?
AgensGraph is an extension of PostgreSQL. So, in AgensGraph a graph is a schema (set of tables with data) and some metadata in tables pg_catalog.ag_label,  pg_catalog.ag_graph and pg_catalog.ag_graphmeta. The data is a set of tables with indexes containing information about vertexes and edges connecting them.

## So, what does it mean to clone a graph?
It, obviously, means to create a **new_schema** that will be a copy of the original schema (lets call it **old_schema** for brevity) and to set up the metadata in the tables mentioned above properly.

### Why that is hard to do?
1. AgensGraph does not have an utility to do that.
2. There are no description in AgensGraph documentation,
3. AgensGraph prohibit many table operations over tables that are part of graph.

### How to copy a schema in PostgreSQL?
Described in [PostgreSQL: How to create full copy of database schema in same database?](https://dba.stackexchange.com/questions/10474/postgresql-how-to-create-full-copy-of-database-schema-in-same-database).
Shortly:
``` bash
psql -U user -d dbname -c 'ALTER SCHEMA old_schema RENAME TO new_schema'
pg_dump -U user -n new_schema -f new_schema.sql dbname
psql -U user -d dbname -c 'ALTER SCHEMA new_schema RENAME TO old_schema'
psql -U user -d dbname -c 'CREATE SCHEMA new_schema'
psql -U user -q -d dbname -f new_schema.sql
rm new_schema.sql
```
In order to do it successfully we need to remove the old_schema from ag_graph table before renaming old_schema and then adding it back before creating new schema - otherwise AgensGraph triggers will prohibit schema from renaming (see Go code).

### What to do with ag_graph table?
```
agens=# \d ag_graph;
            Table "pg_catalog.ag_graph"
  Column   | Type | Collation | Nullable | Default
-----------+------+-----------+----------+---------
 graphname | name |           | not null |
 nspid     | oid  |           | not null |
Indexes:
    "ag_graph_graphname_index" UNIQUE, btree (graphname)
    "ag_graph_oid_index" UNIQUE, btree (oid)
```

graphname == **new_schema**, nspid is taken from
```
agens=# SELECT oid FROM pg_namespace WHERE nspname='new_schema';
  oid
-------
 16418
(1 row)
```

### What to do with ag_label table?
16419 == (nspid+1) of **old_schema**
```
agens=# SELECT * FROM ag_label WHERE graphid=16419;
  labname  | graphid | labid | relid | labkind
-----------+---------+-------+-------+---------
 ag_vertex |   16419 |     1 | 16424 | v
 ag_edge   |   16419 |     2 | 16438 | e
 person    |   16419 |     3 | 16453 | v
 knows     |   16419 |     4 | 16467 | e
(4 rows)
```
We need to copy all those records with the same labname, labid, labkind and new graphid == (nspid+1) of **new_schema** and relid == relfilenode from pg_class:
```
agens=# SELECT relname, relfilenode FROM pg_class WHERE relnamespace=16418;
      relname      | relfilenode
-------------------+-------------
 ag_label_seq      |       16420
 knows             |       16467
 ag_vertex_pkey    |       16433
 ag_vertex_id_seq  |       16435
 ag_edge_id_idx    |       16447
 ag_edge_start_idx |       16448
 ag_edge_end_idx   |       16449
 ag_edge_id_seq    |       16450
 ag_vertex         |       16424
 person            |       16453
 person_pkey       |       16462
 person_id_seq     |       16464
 ag_edge           |       16438
 knows_id_idx      |       16476
 knows_start_idx   |       16477
 knows_end_idx     |       16478
 knows_id_seq      |       16479
(17 rows)
```
**relnamespace** is a nspid of **new_schema**, all the indexes and primary keys need to be ignored, only tables need to be copied.

### What to do with ag_graphmeta table?
I have no idea - it was always empty in my case.

**All of above looks like a plan to me - so, I will try to implement that in a simple Go language utility**.
```
GOPATH=`pwd GOBIN=`pwd` go get github.com/lib/pq
GOPATH=`pwd` GOBIN=`pwd` go build -o ag_clone_graph src/ag_clone_graph.go
$ ./ag_clone_graph --help
Usage of ./ag_clone_graph:
  -dbh string
    	DB Host (default "localhost")
  -dbn string
    	DB Name (default "test")
  -dbport int
    	DB Port (default 5432)
  -dbpsw string
    	DB Password
  -dbu string
    	DB username (default "postgres")
  -g	Debug flag (default true)
  -n string
    	new graph name (default "new_graph")
  -pgpref string
    	directory with pg binaries (default "/usr/local/pgsql/bin/")
  -t string
    	template graph name (default "gtemplate")
```

**[We expect postgres with SSL connection enabled!](https://www.postgresql.org/docs/9.1/ssl-tcp.html)**
