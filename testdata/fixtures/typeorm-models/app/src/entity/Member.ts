import { Check, Column, Entity, Index, JoinColumn, JoinTable, ManyToMany, ManyToOne, OneToOne, PrimaryGeneratedColumn, Unique } from "typeorm";
import { Account } from "./Account";
import { Note } from "./Note";
import { Profile } from "./Profile";
import { Tag } from "./Tag";

// Indexes, constraints and relations (rows TO-X1 to TO-X4,
// TO-R1, TO-R2, TO-R4). The data source has no migrations, so the
// generated names are claimed (TO-X4).
@Entity()
@Index(["nick", "level"])
@Index("idx_member_code", ["code"], { unique: true })
@Unique(["account", "nick"])
@Unique("uq_member_badge", ["badge"])
@Check(`"level" >= 0`)
@Check("chk_member_nick", `"nick" <> ''`)
export class Member {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  code: string;

  @Column()
  badge: string;

  @Index()
  @Column()
  nick: string;

  // A partial unique index: its generated name hashes the
  // predicate.
  @Index({ unique: true, where: `"level" > 0` })
  @Column()
  level: number;

  // An explicit column the join column reuses: its own
  // nullability stays, its type is the referenced key's, and its design
  // type Object (a union under strict) is no failure.
  @Column({ nullable: true })
  accountId: number | null;

  @ManyToOne(() => Account, { onDelete: "CASCADE" })
  @Index()
  account: Account;

  // A composite join column array in array order (0.3.x).
  @ManyToOne(() => Profile, { onUpdate: "cascade", nullable: false })
  @JoinColumn([
    { name: "profile_region", referencedColumnName: "region" },
    { name: "profile_handle", referencedColumnName: "handle" },
  ])
  profile: Profile;

  // A one-to-one owner: a unique constraint on its join column (TO-R2).
  @OneToOne(() => Note)
  @JoinColumn({ name: "pinned_id", foreignKeyConstraintName: "fk_member_pinned" })
  pinned: Note;

  // No foreign key, so no constraint (TO-R1).
  @ManyToOne(() => Account, { createForeignKeyConstraints: false })
  referrer: Promise<Account>;

  // A junction with default names; the inverse side states no action, so
  // the inverse foreign key is NO ACTION on 0.3.x.
  @ManyToMany(() => Tag, (tag) => tag.members, { onDelete: "CASCADE" })
  @JoinTable()
  tags: Tag[];

  // A named junction with named and referenced columns.
  @ManyToMany(() => Account)
  @JoinTable({
    name: "member_follows",
    joinColumn: { name: "member_id", referencedColumnName: "id" },
    inverseJoinColumn: { name: "account_handle", referencedColumnName: "handle" },
  })
  follows: Account[];

  // A self-relation: the junction columns get _1 and _2.
  @ManyToMany(() => Member)
  @JoinTable()
  friends: Member[];
}
