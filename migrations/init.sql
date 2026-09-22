-- 建库。表结构由 GORM AutoMigrate 维护，这里的 DDL 供对照和手工建表使用。
CREATE DATABASE IF NOT EXISTS `startup_copilot`
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `startup_copilot`;

-- 项目：一个用户从模糊想法走到起步的一整段过程，没有账号，token 就是访问凭证
CREATE TABLE IF NOT EXISTS `projects` (
  `id`               CHAR(36)     NOT NULL,
  `created_at`       DATETIME(3)  NULL,
  `updated_at`       DATETIME(3)  NULL,
  `token`            CHAR(32)     NOT NULL,
  `status`           VARCHAR(20)  NOT NULL DEFAULT 'interviewing' COMMENT 'scouting/choosing/interviewing/planned/ended',
  `entry`            VARCHAR(2)   NOT NULL DEFAULT 'A' COMMENT 'A=已有想法 B=不知道做什么',
  `idea`             VARCHAR(500) NULL,
  `city`             VARCHAR(200) NULL,
  `risk_budget`      INT          NOT NULL DEFAULT -1 COMMENT '最多能亏多少元，-1 表示未知',
  `weekly_hours`     INT          NOT NULL DEFAULT -1,
  `assets`           TEXT         NULL COMMENT '入口 B 盘点：手上现成有什么',
  `experience`       TEXT         NULL COMMENT '入口 B 盘点：经历、别人常找他帮的忙',
  `selected_path_id` CHAR(36)     NOT NULL DEFAULT '',
  `verdict`          VARCHAR(10)  NOT NULL DEFAULT '' COMMENT 'go/stop，stop 即劝退',
  `verdict_reason`   TEXT         NULL,
  `asked_count`      INT          NOT NULL DEFAULT 0,
  `ended_at`         DATETIME(3)  NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_projects_token` (`token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 提问阶段的一问一答
CREATE TABLE IF NOT EXISTS `messages` (
  `id`         CHAR(36)    NOT NULL,
  `created_at` DATETIME(3) NULL,
  `updated_at` DATETIME(3) NULL,
  `project_id` CHAR(36)    NOT NULL,
  `role`       VARCHAR(16) NOT NULL COMMENT 'user/assistant',
  `content`    TEXT        NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_messages_project_id` (`project_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 入口 B 的候选路径：固定 3 条，靠 angle 拉开差异
CREATE TABLE IF NOT EXISTS `path_options` (
  `id`           CHAR(36)     NOT NULL,
  `created_at`   DATETIME(3)  NULL,
  `updated_at`   DATETIME(3)  NULL,
  `project_id`   CHAR(36)     NOT NULL,
  `title`        VARCHAR(200) NOT NULL,
  `angle`        VARCHAR(20)  NOT NULL COMMENT 'steady=最稳 fast_cash=最快回钱 high_ceiling=天花板最高',
  `summary`      TEXT         NULL,
  `why_you`      TEXT         NULL COMMENT '为什么用他现成的东西能启动这条',
  `startup_cost` INT          NOT NULL DEFAULT 0 COMMENT '预估启动投入（元），不得超过亏损上限',
  `first_step`   TEXT         NULL,
  `sort_order`   INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_path_options_project_id` (`project_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 方案条目：文档视图渲染全部，任务板只取 yellow/red，同一份数据两个视图
CREATE TABLE IF NOT EXISTS `plan_items` (
  `id`            CHAR(36)     NOT NULL,
  `created_at`    DATETIME(3)  NULL,
  `updated_at`    DATETIME(3)  NULL,
  `project_id`    CHAR(36)     NOT NULL,
  `section`       VARCHAR(50)  NOT NULL,
  `title`         VARCHAR(200) NOT NULL,
  `content`       TEXT         NULL,
  `confidence`    VARCHAR(10)  NOT NULL COMMENT 'green=确定 yellow=我猜的 red=只有你知道',
  `assumption`    TEXT         NULL COMMENT '黄色项的具体假设，给用户去反驳',
  `verify_action` TEXT         NULL COMMENT '今晚两小时能做完的动作',
  `answer`        TEXT         NULL COMMENT '用户回填的真实数据',
  `verified_at`   DATETIME(3)  NULL,
  `sort_order`    INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_plan_items_project_id` (`project_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
