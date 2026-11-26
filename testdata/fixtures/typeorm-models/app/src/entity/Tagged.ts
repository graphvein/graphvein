import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";
import { DbAwareColumn, RenamedColumn, resolveDbType } from "../utils/DbColumnHelper";

@Entity()
export class Tagged {
  @PrimaryGeneratedColumn()
  id: number;

  @DbAwareColumn({ type: "datetime", default: () => "CURRENT_TIMESTAMP" })
  seenAt: Date;

  @DbAwareColumn({ type: "datetime", nullable: true })
  lastAt: Date | null;

  @Column({ type: resolveDbType("datetime"), nullable: true })
  other: Date;

  @RenamedColumn({ nullable: true })
  renamed: string;
}
