/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useRef, useState } from 'react';
import { Button, Card, Form, Spin, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

import {
  API,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';

const { Text, Paragraph } = Typography;

// Backend setting key — see setting/ratio_setting/user_group_discount.go
// (Hermerz/new-api#3). Storage shape is map[string]map[string]float64:
// group → model → discount (0~1, where 0.2 = 2折).
const OPTION_KEY =
  'user_group_model_discount_setting.user_group_model_discount';

const EXAMPLE_JSON = `{
  "default": {
    "gpt-5": 0.2,
    "claude-opus": 0.4
  },
  "enterprise": {
    "gpt-5": 0.4,
    "claude-opus": 0.6
  }
}`;

export default function UserGroupModelDiscountSettings(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [value, setValue] = useState('');
  const [original, setOriginal] = useState('');
  const refForm = useRef();

  useEffect(() => {
    const incoming = props.options?.[OPTION_KEY] ?? '';
    setValue(incoming);
    setOriginal(incoming);
    if (refForm.current) {
      refForm.current.setValues({ [OPTION_KEY]: incoming });
    }
  }, [props.options]);

  async function onSubmit() {
    if (value === original) {
      return showWarning(t('你似乎并没有修改什么'));
    }
    if (value.trim() && !verifyJSON(value)) {
      return showError(t('JSON 格式错误'));
    }

    // Reject discount <= 0 (Hermerz/Hermes#71). Backend mirrors this invariant
    // at settle time: `if appliedUserGroupDiscount > 0` — anything ≤ 0 (including
    // negative and the sentinel 0) silently falls back to GroupRatio. UI must
    // match the backend check so the two stay in sync as one mental model. For
    // free-tier semantics, BD should use a dedicated channel group with
    // GroupRatio = 0 instead.
    if (value.trim()) {
      const parsed = JSON.parse(value);
      for (const group of Object.keys(parsed || {})) {
        const models = parsed[group] || {};
        for (const model of Object.keys(models)) {
          const discount = models[model];
          if (typeof discount === 'number' && discount <= 0) {
            return showError(
              t(
                'discount 必须 > 0（group={{group}}, model={{model}}, 当前值={{value}}）：≤ 0 的值会被后端当作 "未配置" sentinel，悄悄回退到 GroupRatio。免费模型请用专属 channel group + GroupRatio=0。',
                {
                  group,
                  model,
                  value: discount,
                },
              ),
            );
          }
        }
      }
    }

    setLoading(true);
    try {
      const res = await API.put('/api/option/', {
        key: OPTION_KEY,
        value: value,
      });
      if (!res?.data?.success) {
        return showError(res?.data?.message || t('保存失败'));
      }
      showSuccess(t('保存成功'));
      props.refresh?.();
    } catch (e) {
      console.error('save user_group_model_discount failed:', e);
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  function onReset() {
    setValue(original);
    if (refForm.current) {
      refForm.current.setValues({ [OPTION_KEY]: original });
    }
  }

  function loadExample() {
    setValue(EXAMPLE_JSON);
    if (refForm.current) {
      refForm.current.setValues({ [OPTION_KEY]: EXAMPLE_JSON });
    }
  }

  return (
    <Spin spinning={loading} size='large'>
      <Card style={{ marginTop: 8 }}>
        <Typography.Title heading={5}>
          {t('用户分组 × 模型 折扣')}
        </Typography.Title>
        <Paragraph type='tertiary' style={{ marginBottom: 12 }}>
          {t(
            '为「用户分组 × 模型」设置精细化折扣。该折扣 = 客户最终对模型官方价的折扣比例（如 0.2 = 客户调用此模型按官方价 2 折扣费），适用于 2C / 2B 客户分层差异化定价。',
          )}
        </Paragraph>
        <Paragraph type='tertiary' style={{ marginBottom: 12 }}>
          {t('字段含义：')}
          <ul style={{ marginTop: 4 }}>
            <li>
              <Text strong>group</Text>:{' '}
              {t(
                '用户分组名，需与 User 表 group 字段一致（如 default / enterprise）',
              )}
            </li>
            <li>
              <Text strong>model</Text>:{' '}
              {t('模型名，支持 *-openai-compact 通配符（与 ModelRatio 一致）')}
            </li>
            <li>
              <Text strong>discount</Text>:{' '}
              {t(
                '折扣比例 0~1，等于客户对官方价的最终折扣。0.2 = 2 折 of 官方价；0.5 = 5 折。',
              )}
            </li>
          </ul>
        </Paragraph>
        <Paragraph type='tertiary' style={{ marginBottom: 12 }}>
          {t(
            '计费链路（重要）：配了 (group, model) 项 → 最终 ratio = ModelRatio × discount，GroupRatio 被绕过；未配 → 按旧链路 ModelRatio × GroupRatio 计费。这样 BD 在 UI 输入的数字直接等于客户实际折扣，不需要反推 GroupRatio。',
          )}
        </Paragraph>

        <Form
          getFormApi={(formApi) => (refForm.current = formApi)}
          layout='vertical'
        >
          <Form.TextArea
            field={OPTION_KEY}
            label={t('JSON 配置')}
            placeholder={EXAMPLE_JSON}
            value={value}
            onChange={(v) => setValue(v)}
            autosize={{ minRows: 8, maxRows: 24 }}
            style={{ fontFamily: 'monospace' }}
          />
          <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
            <Button theme='solid' type='primary' onClick={onSubmit}>
              {t('保存')}
            </Button>
            <Button onClick={onReset}>{t('重置')}</Button>
            <Button onClick={loadExample}>{t('载入示例')}</Button>
          </div>
        </Form>
      </Card>
    </Spin>
  );
}
