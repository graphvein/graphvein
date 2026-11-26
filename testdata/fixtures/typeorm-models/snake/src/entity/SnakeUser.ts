import { Column, CreateDateColumn, Entity, Index, JoinTable, ManyToMany, ManyToOne, PrimaryGeneratedColumn } from "typeorm";

export enum AdminLevel {
  Default = "default",
  SuperAdmin = "super_admin",
}

@Entity({ name: "users" })
export class SnakeUser {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  firstName: string;

  @Column({ name: "lastName" })
  lastName: string;

  @Column({ type: "enum", enum: AdminLevel, default: AdminLevel.Default })
  adminLevel: AdminLevel;

  @CreateDateColumn()
  createdAt: Date;

  // Snake join column and junction names (TO-K6); the data
  // source has migrations, so generated names are not claimed (TO-X4).
  @Index()
  @ManyToOne(() => SnakeUser)
  invitedBy: SnakeUser;

  @ManyToMany(() => SnakeUser)
  @JoinTable()
  friends: SnakeUser[];
}
