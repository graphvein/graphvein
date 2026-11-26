import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { Beta } from "../entity/beta.entity";

@Module({ imports: [TypeOrmModule.forFeature([Beta])] })
export class BetaModule {}
