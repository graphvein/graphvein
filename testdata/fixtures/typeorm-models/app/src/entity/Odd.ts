import { Check, Column, Entity, Index, JoinTable, ManyToMany, ManyToOne, PrimaryGeneratedColumn, Unique } from "typeorm";
import { Account } from "./Account";
import { Tag } from "./Tag";

// Refusals: the function form of @Index, a deferrable @Unique
// (D), @Index options TypeORM does not create or the contract does not
// read, a property-level @Check (D), a string relation target and a
// junction with join column arrays.
@Entity()
@Index((odd: Odd) => [odd.label])
@Unique(["label"], { deferrable: "INITIALLY DEFERRED" })
export class Odd {
  @PrimaryGeneratedColumn()
  id: number;

  @Index({ synchronize: false })
  @Column()
  label: string;

  @Index({ fulltext: true })
  @Column()
  spot: string;

  @Index({ nullFiltered: true })
  @Column()
  slot: string;

  @Check(`"rank" > 0`)
  @Column()
  rank: number;

  @ManyToOne("Account")
  owner: Account;

  @ManyToMany(() => Tag)
  @JoinTable({ joinColumns: [{ name: "odd_id" }] })
  tags: Tag[];
}
