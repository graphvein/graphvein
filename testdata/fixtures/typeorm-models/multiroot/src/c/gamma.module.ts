import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { Gamma } from "../entity/gamma.entity";

@Module({ imports: [TypeOrmModule.forFeature([Gamma], "lazy")] })
export class GammaModule {}
