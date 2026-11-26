import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { RoleEntity } from "./role.entity";
import { UserEntity } from "./user.entity";

@Module({ imports: [TypeOrmModule.forFeature([UserEntity, RoleEntity])] })
export class UserModule {}
