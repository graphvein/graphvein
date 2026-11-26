import { Column, Entity, ManyToOne, OneToMany, PrimaryGeneratedColumn } from "typeorm";
import { Account } from "./Account";

// The ManyToOne join column is read with the relations.
@Entity({ name: "notes" })
export class Note {
  @PrimaryGeneratedColumn("uuid")
  id: string;

  @Column()
  body: string;

  @ManyToOne(() => Account)
  author: Account;

  @OneToMany(() => Note, (n) => n.parent)
  replies: Note[];

  @ManyToOne(() => Note)
  parent: Note;
}
