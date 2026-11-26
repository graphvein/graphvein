import { Module } from "@nestjs/common";
import { ConfigService } from "@nestjs/config";
import { TypeOrmModule } from "@nestjs/typeorm";
import { DataSource } from "typeorm";
import { Author } from "./entity/Author";
import Post from "./entity/Post";

@Module({
  imports: [
    TypeOrmModule.forRootAsync({
      inject: [ConfigService],
      useFactory: (config: ConfigService) => {
        let logging = config.get("DB_LOGGING");
        try {
          logging = JSON.parse(logging);
        } catch {
          // ignore
        }
        return { ...config.get("database"), autoLoadEntities: true, logging };
      },
      dataSourceFactory: async (options) => {
        const dataSource = await new DataSource(options).initialize();
        return dataSource;
      },
    }),
    TypeOrmModule.forFeature([Post, Author], "default"),
  ],
})
export class AppModule {}
