package xorm

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-xorm/core"
)

// func init() {
// 	RegisterDialect("postgres", &postgres{})
// }

type postgres struct {
	core.Base
}

func (db *postgres) Init(d *core.DB, uri *core.Uri, drivername, dataSourceName string) error {
	return db.Base.Init(d, db, uri, drivername, dataSourceName)
}

func (db *postgres) SqlType(c *core.Column) string {
	var res string
	switch t := c.SQLType.Name; t {
	case core.TinyInt:
		res = core.SmallInt
		return res
	case core.MediumInt, core.Int, core.Integer:
		if c.IsAutoIncrement {
			return core.Serial
		}
		return core.Integer
	case core.Serial, core.BigSerial:
		c.IsAutoIncrement = true
		c.Nullable = false
		res = t
	case core.Binary, core.VarBinary:
		return core.Bytea
	case core.DateTime:
		res = core.TimeStamp
	case core.TimeStampz:
		return "timestamp with time zone"
	case core.Float:
		res = core.Real
	case core.TinyText, core.MediumText, core.LongText:
		res = core.Text
	case core.Uuid:
		res = core.Uuid
	case core.Blob, core.TinyBlob, core.MediumBlob, core.LongBlob:
		return core.Bytea
	case core.Double:
		return "DOUBLE PRECISION"
	default:
		if c.IsAutoIncrement {
			return core.Serial
		}
		res = t
	}

	var hasLen1 bool = (c.Length > 0)
	var hasLen2 bool = (c.Length2 > 0)
	if hasLen2 {
		res += "(" + strconv.Itoa(c.Length) + "," + strconv.Itoa(c.Length2) + ")"
	} else if hasLen1 {
		res += "(" + strconv.Itoa(c.Length) + ")"
	}
	return res
}

func (db *postgres) SupportInsertMany() bool {
	return true
}

func (db *postgres) QuoteStr() string {
	return "\""
}

func (db *postgres) AutoIncrStr() string {
	return ""
}

func (db *postgres) SupportEngine() bool {
	return false
}

func (db *postgres) SupportCharset() bool {
	return false
}

func (db *postgres) IndexOnTable() bool {
	return false
}

func (db *postgres) IndexCheckSql(tableName, idxName string) (string, []interface{}) {
	args := []interface{}{tableName, idxName}
	return `SELECT indexname FROM pg_indexes ` +
		`WHERE tablename = ? AND indexname = ?`, args
}

func (db *postgres) TableCheckSql(tableName string) (string, []interface{}) {
	args := []interface{}{tableName}
	return `SELECT tablename FROM pg_tables WHERE tablename = ?`, args
}

/*func (db *postgres) ColumnCheckSql(tableName, colName string) (string, []interface{}) {
	args := []interface{}{tableName, colName}
	return "SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = ?" +
		" AND column_name = ?", args
}*/

func (db *postgres) ModifyColumnSql(tableName string, col *core.Column) string {
	return fmt.Sprintf("alter table %s ALTER COLUMN %s TYPE %s",
		tableName, col.Name, db.SqlType(col))
}

func (db *postgres) DropIndexSql(tableName string, index *core.Index) string {
	quote := db.Quote
	//var unique string
	var idxName string = index.Name
	if !strings.HasPrefix(idxName, "UQE_") &&
		!strings.HasPrefix(idxName, "IDX_") {
		if index.Type == core.UniqueType {
			idxName = fmt.Sprintf("UQE_%v_%v", tableName, index.Name)
		} else {
			idxName = fmt.Sprintf("IDX_%v_%v", tableName, index.Name)
		}
	}
	return fmt.Sprintf("DROP INDEX %v", quote(idxName))
}

func (db *postgres) IsColumnExist(tableName string, col *core.Column) (bool, error) {
	args := []interface{}{tableName, col.Name}
	query := "SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = $1" +
		" AND column_name = $2"
	rows, err := db.DB().Query(query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	return rows.Next(), nil
}

func (db *postgres) GetColumns(tableName string) ([]string, map[string]*core.Column, error) {
	args := []interface{}{tableName}
	s := `SELECT column_name, column_default, is_nullable, data_type, character_maximum_length, numeric_precision, numeric_precision_radix ,
    CASE WHEN p.contype = 'p' THEN true ELSE false END AS primarykey,
    CASE WHEN p.contype = 'u' THEN true ELSE false END AS uniquekey
FROM pg_attribute f
    JOIN pg_class c ON c.oid = f.attrelid JOIN pg_type t ON t.oid = f.atttypid
    LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum
    LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)
    LEFT JOIN pg_class AS g ON p.confrelid = g.oid
    LEFT JOIN INFORMATION_SCHEMA.COLUMNS s ON s.column_name=f.attname AND c.relname=s.table_name
WHERE c.relkind = 'r'::char AND c.relname = $1 AND f.attnum > 0 ORDER BY f.attnum;`

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	cols := make(map[string]*core.Column)
	colSeq := make([]string, 0)

	for rows.Next() {
		col := new(core.Column)
		col.Indexes = make(map[string]bool)

		var colName, isNullable, dataType string
		var maxLenStr, colDefault, numPrecision, numRadix *string
		var isPK, isUnique bool
		err = rows.Scan(&colName, &colDefault, &isNullable, &dataType, &maxLenStr, &numPrecision, &numRadix, &isPK, &isUnique)
		if err != nil {
			return nil, nil, err
		}
		//fmt.Println(args,colName, isNullable, dataType,maxLenStr, colDefault, numPrecision, numRadix,isPK ,isUnique)
		var maxLen int
		if maxLenStr != nil {
			maxLen, err = strconv.Atoi(*maxLenStr)
			if err != nil {
				return nil, nil, err
			}
		}

		col.Name = strings.Trim(colName, `" `)

		if colDefault != nil || isPK {
			if isPK {
				col.IsPrimaryKey = true
			} else {
				col.Default = *colDefault
			}
		}

		col.Nullable = (isNullable == "YES")

		switch dataType {
		case "character varying", "character":
			col.SQLType = core.SQLType{core.Varchar, 0, 0}
		case "timestamp without time zone":
			col.SQLType = core.SQLType{core.DateTime, 0, 0}
		case "timestamp with time zone":
			col.SQLType = core.SQLType{core.TimeStampz, 0, 0}
		case "double precision":
			col.SQLType = core.SQLType{core.Double, 0, 0}
		case "boolean":
			col.SQLType = core.SQLType{core.Bool, 0, 0}
		case "time without time zone":
			col.SQLType = core.SQLType{core.Time, 0, 0}
		default:
			col.SQLType = core.SQLType{strings.ToUpper(dataType), 0, 0}
		}
		if _, ok := core.SqlTypes[col.SQLType.Name]; !ok {
			return nil, nil, errors.New(fmt.Sprintf("unkonw colType %v", dataType))
		}

		col.Length = maxLen

		if col.SQLType.IsText() || col.SQLType.IsTime() {
			if col.Default != "" {
				col.Default = "'" + col.Default + "'"
			} else {
				if col.DefaultIsEmpty {
					col.Default = "''"
				}
			}
		}
		cols[col.Name] = col
		colSeq = append(colSeq, col.Name)
	}

	return colSeq, cols, nil
}

func (db *postgres) GetTables() ([]*core.Table, error) {
	args := []interface{}{}
	s := "SELECT tablename FROM pg_tables where schemaname = 'public'"

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]*core.Table, 0)
	for rows.Next() {
		table := core.NewEmptyTable()
		var name string
		err = rows.Scan(&name)
		if err != nil {
			return nil, err
		}
		table.Name = name
		tables = append(tables, table)
	}
	return tables, nil
}

func (db *postgres) GetIndexes(tableName string) (map[string]*core.Index, error) {
	args := []interface{}{tableName}
	s := "SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = 'public' and tablename = $1"

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]*core.Index, 0)
	for rows.Next() {
		var indexType int
		var indexName, indexdef string
		var colNames []string
		err = rows.Scan(&indexName, &indexdef)
		if err != nil {
			return nil, err
		}
		indexName = strings.Trim(indexName, `" `)
		if strings.HasSuffix(indexName, "_pkey") {
			continue
		}
		if strings.HasPrefix(indexdef, "CREATE UNIQUE INDEX") {
			indexType = core.UniqueType
		} else {
			indexType = core.IndexType
		}
		cs := strings.Split(indexdef, "(")
		colNames = strings.Split(cs[1][0:len(cs[1])-1], ",")

		if strings.HasPrefix(indexName, "IDX_"+tableName) || strings.HasPrefix(indexName, "UQE_"+tableName) {
			newIdxName := indexName[5+len(tableName) : len(indexName)]
			if newIdxName != "" {
				indexName = newIdxName
			}
		}

		index := &core.Index{Name: indexName, Type: indexType, Cols: make([]string, 0)}
		for _, colName := range colNames {
			index.Cols = append(index.Cols, strings.Trim(colName, `" `))
		}
		indexes[index.Name] = index
	}
	return indexes, nil
}

func (db *postgres) Filters() []core.Filter {
	return []core.Filter{&core.IdFilter{}, &core.QuoteFilter{}, &core.SeqFilter{"$", 1}}
}
