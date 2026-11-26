import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-K1 ormconfig: the package has no data source, so its ormconfig.json
// is read (a cwd-relative glob of the subset).
@Entity()
export class Widget {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  name: string;

  // Revision 4 A2 and B1 on sqlite: a number and a boolean string on a
  // boolean column (stored as written) and a function default.
  @Column({ default: 1 })
  flagNum: boolean;

  @Column("boolean", { default: "false" })
  flagStr: boolean;

  @Column("datetime", { default: () => "datetime('now')" })
  stamped: Date;

  // Revision 4 B1: a function default the oracle fixture does not list
  // is labelled mapped (it is still compared here).
  @Column({ type: "integer", default: () => "(1 + 1)" })
  total: number;

  // Revision 4 A1: a string default on a datetime column is not claimed.
  @Column("datetime", { default: "2020-01-01" })
  since: Date;
}
