/** a utility to clone a graph for AgensGraphDB */
package main;

import (
  "os"
  "log"
  "fmt"
  "flag"
  "errors"
  "strconv"
  "os/exec"
  "database/sql"
  _ "github.com/lib/pq"
)

var (
  dbuser = flag.String("dbu",   "postgres", "DB username")          // or DB_USER
	dbpass = flag.String("dbpsw", "",         "DB Password")		      // or DB_PSW
	dbname = flag.String("dbn",   "test",     "DB Name")	            // or DB_NAME
	dbhost = flag.String("dbh",   "localhost", "DB Host")		          // or DB_HOST
	dbport = flag.Int("dbport",   5432,       "DB Port")		          // or DB_PORT
  pgprefix = flag.String("pgpref", "/usr/local/pgsql/bin/",  "directory with pg binaries")
  debug  = flag.Bool("g",       true,       "Debug flag")

	db    *sql.DB
  initialized = false

  template_name  = flag.String("t", "gtemplate", "template graph name")
  new_graph_name = flag.String("n", "new_graph", "new graph name")
)

func initialize() error {
	flag.Parse()
  //initMu.Lock(); defer initMu.Unlock()
  if initialized { return nil }
  flag.Parse()
	if os.Getenv("DB_USER")!="" { *dbuser = os.Getenv("DB_USER")}
	if os.Getenv("DB_PSW")!=""  { *dbpass = os.Getenv("DB_PSW")}
	if os.Getenv("DB_NAME")!="" { *dbname = os.Getenv("DB_NAME")}
	if os.Getenv("DB_HOST")!="" { *dbhost = os.Getenv("DB_HOST")}
	if os.Getenv("DB_PORT")!="" {
		if p, err := strconv.Atoi(os.Getenv("DB_PORT")); err == nil && p>0 && p<65536 {
			*dbport = p
		}else{
			log.Printf("An error parsing DB_PORT env. variable: ''%s' => '%v', '%d', Using %d",
				os.Getenv("DB_PORT"), err, p, *dbport)
		}
	}
  log.Printf("user=%s host=%s port=%d dbname=%s", *dbuser, *dbhost,  *dbport, *dbname)
	var conn string
  conn = fmt.Sprintf("user=%s host=%s port=%d dbname=%s password=%s sslmode=require",
    *dbuser, *dbhost,  *dbport, *dbname,  *dbpass)
	var err error
	db, err = sql.Open("postgres", conn)
	if err != nil {
		db = nil
		log.Printf("Error '%v' opening postgres DB, conn str is '%s'", err, conn)
		return err
	}
	initialized = true
	log.Println("Connection Opened ...")
  return nil
}

func pg_exec(name string, args []string) error {
  if *debug {log.Printf("Trying to start '%s%s' with params %v", *pgprefix, name,
    append([]string{"-U", *dbuser, "-p", strconv.Itoa(*dbport)}, args...))}
  cmd := exec.Command(fmt.Sprintf("%s%s", *pgprefix, name),
    append([]string{"-U", *dbuser, "-p", strconv.Itoa(*dbport)}, args...)...)
  cmd.Start()
  if len(*dbpass)!=0 {
    if w, err := cmd.StdinPipe(); err == nil {
      fmt.Println(w, *dbpass);
      if err = w.Close(); err!=nil { return err }
      }else { return err }
  }
  return cmd.Wait()
}

func copy_schema(old, new string) error {
  var nspid int64
  if *debug {log.Printf("check if %s already exists", new)}
  row := db.QueryRow(
    fmt.Sprintf(
      `SELECT oid FROM pg_catalog.pg_namespace WHERE nspname='%s';`, new))
  if err := row.Scan(&nspid); err == nil {
    return errors.New(fmt.Sprintf(`Schema (or graph) '%s' already exists!`, new))
  }
  if *debug {log.Printf("check if %s exists", old)}
  row = db.QueryRow(
    fmt.Sprintf(
      `SELECT oid FROM pg_catalog.pg_namespace WHERE nspname='%s';`, old))
  if err := row.Scan(&nspid); err != nil {
    return err
  }
  if *debug {log.Printf("remove %s from ag_graph and rename '%s' to '%s'", old, old, new)}
  if _, err := db.Exec(fmt.Sprintf(
    `DELETE FROM pg_catalog.ag_graph WHERE graphname='%s'; ALTER SCHEMA %s RENAME TO %s;`, old, old, new)); err != nil { return err }
  if *debug {log.Printf("backup newly renamed schema")}
  if err := pg_exec("pg_dump", []string{
    "-n", new, "-f", "/tmp/new_schema.sql", *dbname}); err != nil { return err }
  if *debug {log.Printf("rename the schema back to %s and insert it into ag_graph", old)}
  if _, err := db.Exec(fmt.Sprintf(
    `ALTER SCHEMA %s RENAME TO %s; INSERT INTO ag_graph (graphname, nspid) VALUES ('%s', %d)`,
    new, old, old, nspid)); err != nil { return err }
  if *debug {log.Printf("create new schema")}
  if _, err := db.Exec(fmt.Sprintf(`CREATE SCHEMA %s;`, new)); err != nil { return err }
  if *debug {log.Printf("restore data for new schema")}
  if err := pg_exec("psql", []string{
    "-q", "-d", *dbname, "-f", "/tmp/new_schema.sql", }); err != nil { return err }
  return os.Remove("/tmp/new_schema.sql")
}

func table_work(old, new string) error {
  var nspid, old_nspid int64
  if *debug {log.Printf("read nspid for new")}
  row := db.QueryRow(
    fmt.Sprintf(
      `SELECT oid FROM pg_catalog.pg_namespace WHERE nspname='%s';`, new))
  if err := row.Scan(&nspid); err != nil { return err }
  if *debug {log.Printf("read nspid for old")}
  row = db.QueryRow(
    fmt.Sprintf(
      `SELECT oid FROM pg_catalog.pg_namespace WHERE nspname='%s';`, old))
  if err := row.Scan(&old_nspid); err != nil { return err }
  if *debug {log.Printf("read old labels from ag_label into results map")}
  rows, err := db.Query(
    fmt.Sprintf(
      `SELECT labname, labid, labkind FROM pg_catalog.ag_label WHERE graphid='%d';`, old_nspid+1))
  if err != nil { return err }
  results := make(map[string][]interface{})
  for ;rows.Next(); {
    var labname, labkind string; var labid int;
    if err = rows.Scan(&labname, &labid, &labkind); err != nil { rows.Close(); return err }
    results[labname] = []interface{}{labid, labkind}
  }
  rows.Close()
  if *debug {log.Printf("read new relfilenode from pg_class into results map")}
  rows, err = db.Query(
    fmt.Sprintf(
      `SELECT relname, relfilenode FROM pg_catalog.pg_class WHERE relnamespace='%d';`, nspid))
  if err != nil { return err }
  for ;rows.Next(); {
    var relname string; var relfilenode int;
    if err = rows.Scan(&relname, &relfilenode); err != nil { rows.Close(); return err }
    if res, ok := results[relname]; ok{
      results[relname] = append(res, relfilenode)
    }
  }
  rows.Close()
  if *debug {log.Printf("insert all values into ag_label")}
  for k, v := range results {
    if _, err = db.Exec(fmt.Sprintf(`INSERT INTO pg_catalog.ag_label (labname, graphid, labid, relid, labkind)
    VALUES('%s', %d, %d, %d, '%s')`, k, (nspid+1), v[0].(int), v[2].(int), v[1].(string))); err != nil {
      return err
    }
  }
  if *debug {log.Printf("insert new graph into ag_graph")}
  if _, err := db.Exec(fmt.Sprintf(
    `INSERT INTO ag_graph (graphname, nspid) VALUES ('%s', %d)`,
    new, nspid)); err != nil { return err }
  return nil
}

func main(){
  if err := initialize(); err != nil { return }
  defer db.Close()
  if err := copy_schema(*template_name, *new_graph_name); err!=nil {
    log.Fatalf("Error : %v", err)
  }
  if *debug { log.Printf("Schema %s copied into %s", *template_name, *new_graph_name) }
  if err := table_work(*template_name, *new_graph_name);  err!=nil { log.Fatal(err) }
  if *debug { log.Printf("Graph %s should be a clone of %s", *new_graph_name, *template_name) }
}
