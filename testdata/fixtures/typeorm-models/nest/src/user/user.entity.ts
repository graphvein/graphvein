import { ApiProperty } from "@nestjs/swagger";
import { Exclude } from "class-transformer";
import { Column, CreateDateColumn, Entity, Index, ManyToOne, PrimaryGeneratedColumn } from "typeorm";
import { RoleEntity } from "./role.entity";

@Entity("sys_user")
export class UserEntity {
  @PrimaryGeneratedColumn("uuid")
  id: string;

  @ApiProperty()
  @Column({ length: 32 })
  username: string;

  @Exclude()
  @Column()
  password: string;

  @Column({ type: "tinyint", default: 1 })
  status: number;

  @Column({ name: "is_ext", type: "boolean", default: false })
  isExt: boolean;

  @Column({ type: "int", nullable: true, default: 0 })
  orderNo: number;

  @Column({ type: "datetime", nullable: true })
  lastLogin: Date;

  @Column({ nullable: true, default: "" })
  remark: string;

  @CreateDateColumn({ name: "created_at" })
  createdAt: Date;

  // Revision 4 A3: on MySQL InnoDB adds an index named after the foreign
  // key for a join column no declared index leads with; a declared index
  // covers backupRoleId.
  @ManyToOne(() => RoleEntity)
  mainRole: RoleEntity;

  @Index()
  @ManyToOne(() => RoleEntity, { nullable: true })
  backupRole: RoleEntity;
}
