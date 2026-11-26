import { Check, Column, Entity, JoinTable, ManyToMany, PrimaryGeneratedColumn } from "typeorm";
import { UserEntity } from "./user.entity";

// On MySQL (mapped): a unique column is a unique index named
// IDX_ (EntityMetadataBuilder.js:549-570), a junction table, and a check
// TypeORM never creates there (not read).
@Entity("sys_role")
@Check(`"level" >= 0`)
export class RoleEntity {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ length: 50, unique: true })
  name: string;

  @Column({ default: 0 })
  level: number;

  @ManyToMany(() => UserEntity, { onDelete: "CASCADE" })
  @JoinTable({ name: "sys_user_roles" })
  users: UserEntity[];
}
