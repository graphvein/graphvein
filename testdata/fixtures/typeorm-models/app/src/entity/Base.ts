import { BaseEntity, CreateDateColumn, PrimaryGeneratedColumn, UpdateDateColumn } from "typeorm";

// TO-E2: an abstract base whose members come first; BaseEntity adds none.
export abstract class Base extends BaseEntity {
  @PrimaryGeneratedColumn()
  id: number;

  @CreateDateColumn()
  createdAt: Date;

  @UpdateDateColumn()
  updatedAt: Date;
}
