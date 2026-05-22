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

import { API, showError, showSuccess, showWarning, verifyJSON } from '../../../helpers';

const { Text, Paragraph } = Typography;

// Backend setting key — see setting/ratio_setting/user_group_discount.go
// (Hermerz/new-api#3). Storage shape is map[string]map[string]float64:
// group → model → discount (0~1, where 0.2 = 2折).
const OPTION_KEY = 'user_group_model_discount_setting.user_group_model_discount';

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
            '在全局 ModelRatio 之上为每个用户分组的每个模型再叠一层折扣，用于客户分层差异化定价（如 2C 默认分组享更深折扣，2B 企业分组按更高价计费）。',
          )}
        </Paragraph>
        <Paragraph type='tertiary' style={{ marginBottom: 12 }}>
          {t('字段含义：')}
          <ul style={{ marginTop: 4 }}>
            <li>
              <Text strong>group</Text>:{' '}
              {t('用户分组名，需与 User 表 group 字段一致（如 default / enterprise）')}
            </li>
            <li>
              <Text strong>model</Text>:{' '}
              {t(
                '模型名，支持 *-openai-compact 通配符（与 ModelRatio 一致）',
              )}
            </li>
            <li>
              <Text strong>discount</Text>:{' '}
              {t('折扣比例 0~1，例如 0.2 = 2 折')}
            </li>
          </ul>
        </Paragraph>
        <Paragraph type='tertiary' style={{ marginBottom: 12 }}>
          {t(
            '计费链路：ratio = ModelRatio[model] × UserGroupModelDiscount[group][model] × GroupRatio[channel_group]。空 / 未配置 = 折扣 1.0（沿用旧价）。',
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
