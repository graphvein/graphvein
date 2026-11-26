import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-V7: the schema must exist.
@Entity({ name: "audit_log", schema: "audit" })
export class Audited {
  @PrimaryGeneratedColumn("uuid")
  id: string;

  @Column()
  action: string;
}
