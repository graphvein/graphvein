import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { Alpha } from "../entity/alpha.entity";

@Module({ imports: [TypeOrmModule.forFeature([Alpha])] })
export class AlphaModule {}
