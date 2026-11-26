import { Column, CreateDateColumn, Entity, PrimaryGeneratedColumn, UpdateDateColumn } from "typeorm";

@Entity()
export class Item {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ length: 20, default: "new" })
  label: string;

  @Column({ default: false })
  done: boolean;

  @CreateDateColumn()
  createdAt: Date;

  // A falsy default on a date column: 0.3 replaces it, 1.x keeps it.
  @UpdateDateColumn({ default: 0 })
  updatedAt: Date;
}
