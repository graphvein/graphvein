import { Column, Entity, JoinColumn, JoinTable, ManyToMany, ManyToOne, PrimaryGeneratedColumn } from "typeorm";
import { Reader } from "./Reader";
import { Shelf } from "./Shelf";

// Under 1.x, an explicit composite join column array is sorted
// by the target's primary key; the inverse junction foreign
// key defaults to CASCADE.
@Entity()
export class Book {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  title: string;

  @ManyToOne(() => Shelf, { onDelete: "SET NULL" })
  @JoinColumn([
    { name: "shelf_region", referencedColumnName: "region" },
    { name: "shelf_code", referencedColumnName: "code" },
  ])
  shelf: Shelf;

  @ManyToMany(() => Reader, (reader) => reader.books)
  @JoinTable()
  readers: Reader[];
}
